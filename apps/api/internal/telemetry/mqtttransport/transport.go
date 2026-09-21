// Package mqtttransport owns the Paho/MQTT runtime adapter for telemetry
// ingestion. It has no registry, persistence, or message-validation policy.
package mqtttransport

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/telemetry/ingestion"
)

const (
	defaultQueueSize           = 64
	defaultConnectTimeout      = 5 * time.Second
	defaultSubscribeTimeout    = 5 * time.Second
	defaultReconnectInterval   = 5 * time.Second
	defaultSubscribeRetryDelay = 500 * time.Millisecond
	defaultKeepAlive           = 10 * time.Second
	defaultClientIDPrefix      = "pulsegrid-telemetry-ingestion-"
)

var (
	ErrClientFactoryNil = errors.New("mqtt client factory is required")
	ErrProcessorNil     = errors.New("mqtt telemetry processor is required")
	ErrClientNil        = errors.New("mqtt client initialization failed")
	ErrConnectTimeout   = errors.New("mqtt connection timed out")
	ErrConnectFailed    = errors.New("mqtt connection failed")
	ErrSubscribeTimeout = errors.New("mqtt subscription timed out")
	ErrSubscribeFailed  = errors.New("mqtt subscription failed")
)

// Token is the bounded completion handle exposed by the Paho adapter.
type Token interface {
	WaitTimeout(time.Duration) bool
	Error() error
}

// Message is the small inbound MQTT surface required by the transport.
type Message interface {
	Topic() string
	Payload() []byte
	QoS() byte
	Retained() bool
	Duplicate() bool
}

// Client is the small Paho surface required by the transport.
type Client interface {
	Connect() Token
	Subscribe(topic string, qos byte, handler func(Message)) Token
	Unsubscribe(topics ...string) Token
	Disconnect(quiesce uint)
	IsConnected() bool
}

// ClientFactory creates one fresh client. Paho clients are not reused after
// disconnect; the current runtime relies on the library's auto-reconnect path.
type ClientFactory func(*mqtt.ClientOptions) Client

// Processor handles one delivery. It must return promptly or honor the
// context supplied by the transport so shutdown can drain within its budget.
type Processor func(context.Context, ingestion.Delivery)

// Config contains the bounded local consumer runtime policy.
type Config struct {
	BrokerURL           string
	ClientIDPrefix      string
	QueueSize           int
	ConnectTimeout      time.Duration
	SubscribeTimeout    time.Duration
	ReconnectInterval   time.Duration
	SubscribeRetryDelay time.Duration
	KeepAlive           time.Duration
}

// DefaultConfig returns the reviewed MVP-005 local runtime policy.
func DefaultConfig(brokerURL string) Config {
	return Config{
		BrokerURL:           brokerURL,
		ClientIDPrefix:      defaultClientIDPrefix,
		QueueSize:           defaultQueueSize,
		ConnectTimeout:      defaultConnectTimeout,
		SubscribeTimeout:    defaultSubscribeTimeout,
		ReconnectInterval:   defaultReconnectInterval,
		SubscribeRetryDelay: defaultSubscribeRetryDelay,
		KeepAlive:           defaultKeepAlive,
	}
}

func (c *Config) applyDefaults() {
	if c.ClientIDPrefix == "" {
		c.ClientIDPrefix = defaultClientIDPrefix
	}
	if c.QueueSize <= 0 {
		c.QueueSize = defaultQueueSize
	}
	if c.ConnectTimeout <= 0 {
		c.ConnectTimeout = defaultConnectTimeout
	}
	if c.SubscribeTimeout <= 0 {
		c.SubscribeTimeout = defaultSubscribeTimeout
	}
	if c.ReconnectInterval <= 0 {
		c.ReconnectInterval = defaultReconnectInterval
	}
	if c.SubscribeRetryDelay <= 0 {
		c.SubscribeRetryDelay = defaultSubscribeRetryDelay
	}
	if c.KeepAlive <= 0 {
		c.KeepAlive = defaultKeepAlive
	}
}

// Transport owns one bounded inbound queue and one processing worker.
type Transport struct {
	config    Config
	factory   ClientFactory
	processor Processor
	logger    *slog.Logger

	queue             chan ingestion.Delivery
	workerDone        chan struct{}
	stopAdmission     chan struct{}
	forceStop         chan struct{}
	stopAdmissionOnce sync.Once
	stopOnce          sync.Once
	forceStopOnce     sync.Once
	firstReady        chan struct{}
	firstReadyOnce    sync.Once

	connectionContext context.Context
	connectionCancel  context.CancelFunc
	processContext    context.Context
	processCancel     context.CancelFunc

	clientMu sync.RWMutex
	client   Client

	admissionMu sync.Mutex
	accepting   bool

	generation atomic.Uint64
	ready      atomic.Bool
	started    atomic.Bool

	stopResult error
}

// New creates a stopped MQTT transport.
func New(config Config, factory ClientFactory, processor Processor, logger *slog.Logger) (*Transport, error) {
	if factory == nil {
		return nil, ErrClientFactoryNil
	}
	if processor == nil {
		return nil, ErrProcessorNil
	}
	if config.BrokerURL == "" {
		return nil, errors.New("mqtt broker URL is required")
	}
	config.applyDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &Transport{
		config:        config,
		factory:       factory,
		processor:     processor,
		logger:        logger,
		queue:         make(chan ingestion.Delivery, config.QueueSize),
		workerDone:    make(chan struct{}),
		stopAdmission: make(chan struct{}),
		forceStop:     make(chan struct{}),
		firstReady:    make(chan struct{}),
		accepting:     false,
	}, nil
}

// Start connects and waits for the first successful subscription. Later
// disconnects keep the process alive but clear readiness while Paho retries.
func (t *Transport) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !t.started.CompareAndSwap(false, true) {
		return errors.New("mqtt transport has already started")
	}

	t.connectionContext, t.connectionCancel = context.WithCancel(context.Background())
	t.processContext, t.processCancel = context.WithCancel(context.Background())
	t.admissionMu.Lock()
	t.accepting = true
	t.admissionMu.Unlock()
	go t.processQueue()

	var current Client
	options := mqtt.NewClientOptions().
		AddBroker(t.config.BrokerURL).
		SetClientID(t.config.ClientIDPrefix + uuid.NewString()).
		SetProtocolVersion(4).
		SetCleanSession(true).
		SetOrderMatters(true).
		SetAutoReconnect(true).
		SetConnectRetry(false).
		SetConnectTimeout(t.config.ConnectTimeout).
		SetMaxReconnectInterval(t.config.ReconnectInterval).
		SetKeepAlive(t.config.KeepAlive).
		SetOnConnectHandler(func(_ mqtt.Client) {
			if current != nil {
				t.handleConnect(current)
			}
		}).
		SetConnectionLostHandler(func(_ mqtt.Client, _ error) {
			t.ready.Store(false)
			t.generation.Add(1)
			t.logger.Warn("mqtt ingestion disconnected", "reason_code", "mqtt_disconnected")
		})

	current = t.factory(options)
	if current == nil {
		return t.failStart(ErrClientNil)
	}
	t.clientMu.Lock()
	t.client = current
	t.clientMu.Unlock()

	if err := waitToken(ctx, current.Connect(), t.config.ConnectTimeout, ErrConnectTimeout, ErrConnectFailed); err != nil {
		return t.failStart(err)
	}
	readyTimer := time.NewTimer(t.config.SubscribeTimeout)
	defer readyTimer.Stop()
	select {
	case <-t.firstReady:
		return nil
	case <-ctx.Done():
		return t.failStart(ctx.Err())
	case <-readyTimer.C:
		return t.failStart(ErrSubscribeTimeout)
	}
}

// Stop prevents admission, disconnects MQTT, and drains queued work until ctx
// expires. It is safe to call more than once.
func (t *Transport) Stop(ctx context.Context) error {
	if !t.started.Load() {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	t.stopOnce.Do(func() {
		t.stopResult = t.stop(ctx)
	})
	return t.stopResult
}

func (t *Transport) stop(ctx context.Context) error {
	t.admissionMu.Lock()
	t.accepting = false
	t.admissionMu.Unlock()
	t.ready.Store(false)
	t.generation.Add(1)
	if t.connectionCancel != nil {
		t.connectionCancel()
	}
	t.stopAdmissionOnce.Do(func() { close(t.stopAdmission) })

	var cleanupErr error
	if client := t.currentClient(); client != nil {
		if token := client.Unsubscribe(ingestion.TelemetryTopicFilter); token != nil {
			cleanupErr = waitToken(ctx, token, t.config.SubscribeTimeout, ErrSubscribeTimeout, ErrSubscribeFailed)
		}
		client.Disconnect(quiesceMilliseconds(ctx))
	}

	select {
	case <-t.workerDone:
		t.processCancel()
		if cleanupErr != nil {
			return cleanupErr
		}
		return nil
	case <-ctx.Done():
		t.processCancel()
		t.forceStopOnce.Do(func() { close(t.forceStop) })
		if cleanupErr != nil {
			return errors.Join(ctx.Err(), cleanupErr)
		}
		return ctx.Err()
	}
}

// Ready is true only after a connection and its subscription are active.
func (t *Transport) Ready() bool {
	return t.ready.Load()
}

func (t *Transport) currentClient() Client {
	t.clientMu.RLock()
	defer t.clientMu.RUnlock()
	return t.client
}

func (t *Transport) handleConnect(client Client) {
	t.ready.Store(false)
	generation := t.generation.Add(1)
	go t.subscribeUntilReady(client, generation)
}

func (t *Transport) subscribeUntilReady(client Client, generation uint64) {
	for {
		if t.connectionContext.Err() != nil || generation != t.generation.Load() || !client.IsConnected() {
			return
		}
		token := client.Subscribe(ingestion.TelemetryTopicFilter, 1, t.handleMessage)
		err := waitToken(t.connectionContext, token, t.config.SubscribeTimeout, ErrSubscribeTimeout, ErrSubscribeFailed)
		if err == nil {
			t.ready.Store(true)
			t.firstReadyOnce.Do(func() { close(t.firstReady) })
			return
		}
		t.logger.Warn("mqtt ingestion subscription unavailable", "reason_code", "mqtt_subscription_unavailable")
		timer := time.NewTimer(t.config.SubscribeRetryDelay)
		select {
		case <-timer.C:
		case <-t.connectionContext.Done():
			timer.Stop()
			return
		}
	}
}

func (t *Transport) handleMessage(message Message) {
	if message == nil {
		t.logger.Warn("mqtt telemetry callback received no message", "reason_code", "mqtt_message_missing")
		return
	}
	delivery := ingestion.Delivery{
		IngestionID: uuid.New(),
		Topic:       message.Topic(),
		Payload:     append([]byte(nil), message.Payload()...),
		QoS:         message.QoS(),
		Retained:    message.Retained(),
		Duplicate:   message.Duplicate(),
		ReceivedAt:  time.Now().UTC(),
	}

	t.admissionMu.Lock()
	defer t.admissionMu.Unlock()
	if !t.accepting {
		return
	}
	select {
	case t.queue <- delivery:
	default:
		t.logger.Warn("mqtt telemetry queue is full", "reason_code", "ingestion_overloaded", "ingestion_id", delivery.IngestionID, "payload_bytes", len(delivery.Payload))
	}
}

func (t *Transport) processQueue() {
	defer close(t.workerDone)
	var stopChannel <-chan struct{} = t.stopAdmission
	draining := false
	for {
		if draining && len(t.queue) == 0 {
			return
		}
		select {
		case delivery := <-t.queue:
			t.processor(t.processContext, delivery)
		case <-stopChannel:
			draining = true
			stopChannel = nil
		case <-t.forceStop:
			return
		}
	}
}

func (t *Transport) failStart(err error) error {
	t.admissionMu.Lock()
	t.accepting = false
	t.admissionMu.Unlock()
	t.ready.Store(false)
	if t.connectionCancel != nil {
		t.connectionCancel()
	}
	if t.processCancel != nil {
		t.processCancel()
	}
	t.stopAdmissionOnce.Do(func() { close(t.stopAdmission) })
	t.forceStopOnce.Do(func() { close(t.forceStop) })
	if client := t.currentClient(); client != nil {
		client.Disconnect(0)
	}
	select {
	case <-t.workerDone:
	case <-time.After(time.Second):
	}
	return err
}

func waitToken(ctx context.Context, token Token, timeout time.Duration, timeoutErr error, operationErr error) error {
	if token == nil {
		return operationErr
	}
	result := make(chan error, 1)
	go func() {
		if !token.WaitTimeout(timeout) {
			result <- timeoutErr
			return
		}
		if err := token.Error(); err != nil {
			result <- operationErr
			return
		}
		result <- nil
	}()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func quiesceMilliseconds(ctx context.Context) uint {
	deadline, ok := ctx.Deadline()
	if !ok {
		return 0
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 0
	}
	milliseconds := remaining / time.Millisecond
	maxUint := ^uint(0)
	if uint64(milliseconds) > uint64(maxUint) {
		return maxUint
	}
	return uint(milliseconds)
}
