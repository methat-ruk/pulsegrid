// Package mqtttransport owns the API's isolated MQTT command client. It copies
// response deliveries into a bounded queue and never applies domain state.
package mqtttransport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/commands/mqttcontract"
)

const (
	defaultQueueSize        = 64
	defaultConnectTimeout   = 5 * time.Second
	defaultSubscribeTimeout = 5 * time.Second
	defaultWriteTimeout     = 2 * time.Second
	defaultPublishTimeout   = 3 * time.Second
	defaultReconnectDelay   = 500 * time.Millisecond
	maximumReconnectDelay   = 5 * time.Second
	maximumTopicBytes       = 256
	clientIDPrefix          = "pulsegrid-command-delivery-"
)

var (
	ErrClientFactoryNil = errors.New("command MQTT client factory is required")
	ErrClientNil        = errors.New("command MQTT client initialization failed")
	ErrUnavailable      = errors.New("command MQTT transport is unavailable")
	ErrConnectTimeout   = errors.New("command MQTT connect timed out")
	ErrConnectFailed    = errors.New("command MQTT connect failed")
	ErrSubscribeTimeout = errors.New("command MQTT subscribe timed out")
	ErrSubscribeFailed  = errors.New("command MQTT subscribe failed")
	ErrPublishTimeout   = errors.New("command MQTT publish acknowledgement timed out")
	ErrPublishFailed    = errors.New("command MQTT publish failed")
)

type Token interface {
	WaitTimeout(time.Duration) bool
	Error() error
}

type SubscribeToken interface {
	Token
	Result() map[string]byte
}

type Message interface {
	Topic() string
	Payload() []byte
	QoS() byte
	Retained() bool
	Duplicate() bool
}

type Client interface {
	Connect() Token
	Subscribe(string, byte, func(Message)) SubscribeToken
	Publish(string, byte, bool, any) Token
	Disconnect(uint)
	IsConnectionOpen() bool
}

type ClientFactory func(*mqtt.ClientOptions) Client

type Config struct {
	BrokerURL             string
	ResponseFilter        string
	QueueSize             int
	ConnectTimeout        time.Duration
	SubscribeTimeout      time.Duration
	WriteTimeout          time.Duration
	PublishTimeout        time.Duration
	ReconnectDelay        time.Duration
	MaximumReconnectDelay time.Duration
}

func DefaultConfig(brokerURL string) Config {
	filter, _ := mqttcontract.ResponseFilter("pulsegrid-dev")
	return Config{
		BrokerURL: brokerURL, ResponseFilter: filter, QueueSize: defaultQueueSize,
		ConnectTimeout: defaultConnectTimeout, SubscribeTimeout: defaultSubscribeTimeout,
		WriteTimeout: defaultWriteTimeout, PublishTimeout: defaultPublishTimeout,
		ReconnectDelay: defaultReconnectDelay, MaximumReconnectDelay: maximumReconnectDelay,
	}
}

type Delivery struct {
	ID           uuid.UUID
	Topic        string
	Payload      []byte
	PayloadBytes int
	QoS          byte
	Retained     bool
	Duplicate    bool
	Generation   uint64
	ReceivedAt   time.Time
	Oversized    bool
	TopicInvalid bool
}

type Transport struct {
	config  Config
	factory ClientFactory
	logger  *slog.Logger

	queue       chan Delivery
	retire      chan uint64
	ctx         context.Context
	cancel      context.CancelFunc
	admissionMu sync.RWMutex
	clientMu    sync.RWMutex
	client      Client
	clientEpoch uint64
	publishMu   sync.Mutex

	ready      atomic.Bool
	accepting  atomic.Bool
	started    atomic.Bool
	generation atomic.Uint64

	done     chan struct{}
	doneOnce sync.Once
}

func New(config Config, factory ClientFactory, logger *slog.Logger) (*Transport, error) {
	if factory == nil {
		return nil, ErrClientFactoryNil
	}
	if config.BrokerURL == "" || config.ResponseFilter == "" {
		return nil, errors.New("command MQTT broker and response filter are required")
	}
	applyDefaults(&config)
	if logger == nil {
		logger = slog.Default()
	}
	return &Transport{
		config: config, factory: factory, logger: logger,
		queue: make(chan Delivery, config.QueueSize), retire: make(chan uint64, 1),
		done: make(chan struct{}),
	}, nil
}

func applyDefaults(config *Config) {
	defaults := DefaultConfig(config.BrokerURL)
	if config.ResponseFilter == "" {
		config.ResponseFilter = defaults.ResponseFilter
	}
	if config.QueueSize <= 0 {
		config.QueueSize = defaults.QueueSize
	}
	if config.ConnectTimeout <= 0 {
		config.ConnectTimeout = defaults.ConnectTimeout
	}
	if config.SubscribeTimeout <= 0 {
		config.SubscribeTimeout = defaults.SubscribeTimeout
	}
	if config.WriteTimeout <= 0 {
		config.WriteTimeout = defaults.WriteTimeout
	}
	if config.PublishTimeout <= 0 {
		config.PublishTimeout = defaults.PublishTimeout
	}
	if config.ReconnectDelay <= 0 {
		config.ReconnectDelay = defaults.ReconnectDelay
	}
	if config.MaximumReconnectDelay <= 0 {
		config.MaximumReconnectDelay = defaults.MaximumReconnectDelay
	}
}

// Start connects and establishes the response subscription before returning.
// Later broker failures are handled by the owned reconnect supervisor.
func (t *Transport) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !t.started.CompareAndSwap(false, true) {
		return errors.New("command MQTT transport has already started")
	}
	t.ctx, t.cancel = context.WithCancel(ctx)
	t.accepting.Store(true)
	client, epoch, err := t.connectEpoch(t.ctx)
	if err != nil {
		t.stopAdmission()
		t.cancel()
		if client != nil {
			client.Disconnect(0)
		}
		t.closeDone()
		return err
	}
	t.install(client, epoch)
	t.ready.Store(true)
	go t.supervise(client, epoch)
	return nil
}

func (t *Transport) Ready() bool { return t.ready.Load() }

func (t *Transport) Generation() uint64 { return t.generation.Load() }

func (t *Transport) Deliveries() <-chan Delivery { return t.queue }

// stopAdmission closes response admission and waits for callbacks that already
// entered the admission boundary to finish queueing or rejecting their message.
func (t *Transport) stopAdmission() {
	t.admissionMu.Lock()
	t.accepting.Store(false)
	t.admissionMu.Unlock()
}

// Publish waits for broker acknowledgement within the caller and configured
// budgets. An uncertain result retires the client epoch; it is never retried
// inside this adapter.
func (t *Transport) Publish(ctx context.Context, topic string, payload []byte) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(payload) == 0 || len(payload) > mqttcontract.MaxPayloadBytes || topic == "" {
		return errors.New("command MQTT publish input is invalid")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	t.publishMu.Lock()
	defer t.publishMu.Unlock()
	if !t.ready.Load() || !t.accepting.Load() {
		return ErrUnavailable
	}
	t.clientMu.RLock()
	client, epoch := t.client, t.clientEpoch
	t.clientMu.RUnlock()
	if client == nil || !client.IsConnectionOpen() {
		t.retireEpoch(epoch)
		return ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	token := client.Publish(topic, 1, false, append([]byte(nil), payload...))
	if token == nil {
		t.retireEpoch(epoch)
		return ErrPublishFailed
	}
	wait := boundedWait(ctx, t.config.PublishTimeout)
	if wait <= 0 || !token.WaitTimeout(wait) {
		t.retireEpoch(epoch)
		return ErrPublishTimeout
	}
	if err := token.Error(); err != nil {
		t.retireEpoch(epoch)
		return fmt.Errorf("%w: %v", ErrPublishFailed, err)
	}
	if err := ctx.Err(); err != nil {
		t.retireEpoch(epoch)
		return err
	}
	return nil
}

func (t *Transport) Stop(ctx context.Context) error {
	if !t.started.Load() {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	t.stopAdmission()
	t.ready.Store(false)
	t.cancel()
	select {
	case <-t.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *Transport) connectEpoch(ctx context.Context) (Client, uint64, error) {
	epoch := t.generation.Add(1)
	options := mqtt.NewClientOptions().
		AddBroker(t.config.BrokerURL).
		SetClientID(clientIDPrefix + uuid.NewString()).
		SetProtocolVersion(4).
		SetCleanSession(true).
		SetOrderMatters(false).
		SetAutoReconnect(false).
		SetConnectRetry(false).
		SetConnectTimeout(t.config.ConnectTimeout).
		SetWriteTimeout(t.config.WriteTimeout).
		SetKeepAlive(10 * time.Second)
	client := t.factory(options)
	if client == nil {
		return nil, epoch, ErrClientNil
	}
	if err := waitToken(ctx, client.Connect(), t.config.ConnectTimeout, ErrConnectTimeout, ErrConnectFailed); err != nil {
		return client, epoch, err
	}
	token := client.Subscribe(t.config.ResponseFilter, 1, func(message Message) {
		t.handleMessage(epoch, message)
	})
	if token == nil {
		return client, epoch, ErrSubscribeFailed
	}
	if err := waitToken(ctx, token, t.config.SubscribeTimeout, ErrSubscribeTimeout, ErrSubscribeFailed); err != nil {
		return client, epoch, err
	}
	grants := token.Result()
	if len(grants) != 1 || grants[t.config.ResponseFilter] != 1 {
		return client, epoch, ErrSubscribeFailed
	}
	if !client.IsConnectionOpen() {
		return client, epoch, ErrUnavailable
	}
	return client, epoch, nil
}

func (t *Transport) supervise(client Client, epoch uint64) {
	defer t.closeDone()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-t.ctx.Done():
			t.retireEpoch(epoch)
			return
		case retiredEpoch := <-t.retire:
			if retiredEpoch != epoch {
				continue
			}
			client, epoch = nil, 0
		case <-ticker.C:
			if client != nil && !client.IsConnectionOpen() {
				t.retireEpoch(epoch)
				client, epoch = nil, 0
			}
		}
		if client != nil || t.ctx.Err() != nil {
			continue
		}
		var err error
		client, epoch, err = t.reconnect()
		if err != nil {
			return
		}
	}
}

func (t *Transport) reconnect() (Client, uint64, error) {
	delay := t.config.ReconnectDelay
	for t.ctx.Err() == nil {
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-t.ctx.Done():
			timer.Stop()
			return nil, 0, t.ctx.Err()
		}
		client, epoch, err := t.connectEpoch(t.ctx)
		if err == nil {
			t.install(client, epoch)
			t.ready.Store(true)
			t.logger.Info("command MQTT response subscription ready", "reason_code", "command_mqtt_ready", "epoch", epoch)
			return client, epoch, nil
		}
		if client != nil {
			client.Disconnect(500)
		}
		t.logger.Warn("command MQTT reconnect unavailable", "reason_code", "command_mqtt_reconnect_failed", "epoch", epoch)
		if delay < t.config.MaximumReconnectDelay/2 {
			delay *= 2
		} else {
			delay = t.config.MaximumReconnectDelay
		}
	}
	return nil, 0, t.ctx.Err()
}

func (t *Transport) install(client Client, epoch uint64) {
	t.clientMu.Lock()
	t.client, t.clientEpoch = client, epoch
	t.clientMu.Unlock()
}

func (t *Transport) retireEpoch(epoch uint64) {
	if epoch == 0 {
		return
	}
	t.clientMu.Lock()
	if t.client == nil || t.clientEpoch != epoch {
		t.clientMu.Unlock()
		return
	}
	client := t.client
	t.client, t.clientEpoch = nil, 0
	t.ready.Store(false)
	t.generation.Add(1)
	t.clientMu.Unlock()
	client.Disconnect(0)
	select {
	case t.retire <- epoch:
	default:
	}
	t.logger.Warn("command MQTT connection retired", "reason_code", "command_mqtt_disconnected", "epoch", epoch)
}

func (t *Transport) handleMessage(epoch uint64, message Message) {
	t.admissionMu.RLock()
	defer t.admissionMu.RUnlock()
	if message == nil || !t.accepting.Load() || epoch != t.generation.Load() {
		return
	}
	topic := message.Topic()
	delivery := Delivery{
		ID: uuid.New(), QoS: message.QoS(), Retained: message.Retained(),
		Duplicate: message.Duplicate(), Generation: epoch, ReceivedAt: time.Now().UTC(),
	}
	if len(topic) > maximumTopicBytes {
		delivery.TopicInvalid = true
	} else {
		delivery.Topic = stringsClone(topic)
	}
	payload := message.Payload()
	delivery.PayloadBytes = len(payload)
	if len(payload) > mqttcontract.MaxPayloadBytes {
		delivery.Oversized = true
	} else {
		delivery.Payload = append([]byte(nil), payload...)
	}
	select {
	case t.queue <- delivery:
	default:
		t.logger.Warn("command MQTT response queue full", "reason_code", "command_response_overloaded", "delivery_id", delivery.ID, "payload_bytes", delivery.PayloadBytes)
	}
}

func waitToken(ctx context.Context, token Token, timeout time.Duration, timeoutErr, operationErr error) error {
	if token == nil {
		return operationErr
	}
	wait := boundedWait(ctx, timeout)
	if wait <= 0 || !token.WaitTimeout(wait) {
		if err := ctx.Err(); err != nil {
			return err
		}
		return timeoutErr
	}
	if err := token.Error(); err != nil {
		return fmt.Errorf("%w: %v", operationErr, err)
	}
	return nil
}

func boundedWait(ctx context.Context, maximum time.Duration) time.Duration {
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < maximum {
			return remaining
		}
	}
	return maximum
}

func stringsClone(value string) string { return string(append([]byte(nil), value...)) }

func (t *Transport) closeDone() { t.doneOnce.Do(func() { close(t.done) }) }

func PahoClientFactory(options *mqtt.ClientOptions) Client {
	return pahoClient{client: mqtt.NewClient(options)}
}

type pahoClient struct{ client mqtt.Client }

func (c pahoClient) Connect() Token { return pahoToken{token: c.client.Connect()} }

func (c pahoClient) Subscribe(topic string, qos byte, handler func(Message)) SubscribeToken {
	token := c.client.Subscribe(topic, qos, func(_ mqtt.Client, message mqtt.Message) { handler(pahoMessage{message}) })
	return pahoSubscribeToken{pahoToken: pahoToken{token: token}, token: token}
}

func (c pahoClient) Publish(topic string, qos byte, retained bool, payload any) Token {
	return pahoToken{token: c.client.Publish(topic, qos, retained, payload)}
}

func (c pahoClient) Disconnect(quiesce uint) { c.client.Disconnect(quiesce) }

func (c pahoClient) IsConnectionOpen() bool { return c.client.IsConnectionOpen() }

type pahoToken struct{ token mqtt.Token }

func (t pahoToken) WaitTimeout(timeout time.Duration) bool { return t.token.WaitTimeout(timeout) }

func (t pahoToken) Error() error { return t.token.Error() }

type pahoSubscribeToken struct {
	pahoToken
	token mqtt.Token
}

func (t pahoSubscribeToken) Result() map[string]byte {
	if result, ok := t.token.(*mqtt.SubscribeToken); ok {
		return result.Result()
	}
	return nil
}

type pahoMessage struct{ message mqtt.Message }

func (m pahoMessage) Topic() string   { return m.message.Topic() }
func (m pahoMessage) Payload() []byte { return m.message.Payload() }
func (m pahoMessage) QoS() byte       { return m.message.Qos() }
func (m pahoMessage) Retained() bool  { return m.message.Retained() }
func (m pahoMessage) Duplicate() bool { return m.message.Duplicate() }
