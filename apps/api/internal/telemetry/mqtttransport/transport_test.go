package mqtttransport

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/telemetry/ingestion"
)

type fakeToken struct {
	err       error
	complete  bool
	afterWait func()
}

func (t fakeToken) WaitTimeout(time.Duration) bool {
	if t.afterWait != nil {
		t.afterWait()
	}
	return t.complete
}
func (t fakeToken) Error() error { return t.err }

type fakeMessage struct {
	topic     string
	payload   []byte
	qos       byte
	retained  bool
	duplicate bool
}

func (m fakeMessage) Topic() string   { return m.topic }
func (m fakeMessage) Payload() []byte { return m.payload }
func (m fakeMessage) QoS() byte       { return m.qos }
func (m fakeMessage) Retained() bool  { return m.retained }
func (m fakeMessage) Duplicate() bool { return m.duplicate }

type fakeClient struct {
	options *mqtt.ClientOptions

	mu                 sync.Mutex
	connected          bool
	handler            func(Message)
	connectErr         error
	subscribeErr       error
	connectIncomplete  bool
	afterSubscribe     func()
	unsubscribeStarted chan struct{}
	unsubscribeBlock   <-chan struct{}
	unsubscribeOnce    sync.Once
	disconnected       bool
}

func (c *fakeClient) Connect() Token {
	c.mu.Lock()
	connectErr := c.connectErr
	if connectErr == nil {
		c.connected = true
	}
	c.mu.Unlock()
	if connectErr != nil {
		return fakeToken{complete: true, err: connectErr}
	}
	if c.options.OnConnect != nil {
		c.options.OnConnect(nil)
	}
	return fakeToken{complete: !c.connectIncomplete}
}

func (c *fakeClient) Subscribe(_ string, _ byte, handler func(Message)) Token {
	c.mu.Lock()
	subscribeErr := c.subscribeErr
	afterSubscribe := c.afterSubscribe
	c.afterSubscribe = nil
	if subscribeErr == nil {
		c.handler = handler
	}
	c.mu.Unlock()
	if subscribeErr != nil {
		return fakeToken{complete: true, err: subscribeErr}
	}
	return fakeToken{complete: true, afterWait: afterSubscribe}
}

func (c *fakeClient) Unsubscribe(...string) Token {
	if c.unsubscribeStarted != nil {
		c.unsubscribeOnce.Do(func() { close(c.unsubscribeStarted) })
	}
	if c.unsubscribeBlock != nil {
		<-c.unsubscribeBlock
	}
	return fakeToken{complete: true}
}

func (c *fakeClient) Disconnect(uint) {
	c.mu.Lock()
	c.connected = false
	c.disconnected = true
	c.mu.Unlock()
}

func (c *fakeClient) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

func (c *fakeClient) Emit(message Message) {
	c.mu.Lock()
	handler := c.handler
	c.mu.Unlock()
	if handler != nil {
		handler(message)
	}
}

func (c *fakeClient) Drop() {
	c.mu.Lock()
	c.connected = false
	c.mu.Unlock()
	if c.options.OnConnectionLost != nil {
		c.options.OnConnectionLost(nil, errors.New("connection lost"))
	}
}

func (c *fakeClient) Reconnect() {
	c.mu.Lock()
	c.connected = true
	c.mu.Unlock()
	if c.options.OnConnect != nil {
		c.options.OnConnect(nil)
	}
}

func TestTransportStartsSubscribesProcessesAndStops(t *testing.T) {
	client := &fakeClient{}
	processed := make(chan ingestion.Delivery, 1)
	transport, err := New(DefaultConfig("mqtt://127.0.0.1:11883"), func(options *mqtt.ClientOptions) Client {
		client.options = options
		return client
	}, func(_ context.Context, delivery ingestion.Delivery) {
		processed <- delivery
	}, nil)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	startContext, cancelStart := context.WithTimeout(context.Background(), time.Second)
	defer cancelStart()
	if err := transport.Start(startContext); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if !transport.Ready() {
		t.Fatal("transport is not ready after successful subscription")
	}
	if client.options.WriteTimeout != 5*time.Second {
		t.Fatalf("Paho write timeout = %s, want 5s", client.options.WriteTimeout)
	}

	client.Emit(fakeMessage{topic: "topic", payload: []byte("payload"), qos: 1})
	select {
	case delivery := <-processed:
		if delivery.IngestionID == uuid.Nil || string(delivery.Payload) != "payload" || delivery.QoS != 1 {
			t.Fatalf("processed delivery = %+v", delivery)
		}
	case <-time.After(time.Second):
		t.Fatal("delivery was not processed")
	}

	stopContext, cancelStop := context.WithTimeout(context.Background(), time.Second)
	defer cancelStop()
	if err := transport.Stop(stopContext); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if transport.Ready() || !client.disconnected {
		t.Fatalf("transport ready=%t disconnected=%t after stop", transport.Ready(), client.disconnected)
	}
}

func TestTransportClearsReadinessAndResubscribesAfterReconnect(t *testing.T) {
	client := &fakeClient{}
	transport, err := New(DefaultConfig("mqtt://127.0.0.1:1883"), func(options *mqtt.ClientOptions) Client {
		client.options = options
		return client
	}, func(context.Context, ingestion.Delivery) {}, nil)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	client.Drop()
	waitFor(t, func() bool { return !transport.Ready() })
	client.Reconnect()
	waitFor(t, transport.Ready)
	stopContext, cancelStop := context.WithTimeout(context.Background(), time.Second)
	defer cancelStop()
	if err := transport.Stop(stopContext); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
}

func TestTransportFailsFastWhenInitialConnectionFails(t *testing.T) {
	client := &fakeClient{connectErr: errors.New("connection refused")}
	transport, err := New(DefaultConfig("mqtt://127.0.0.1:1883"), func(options *mqtt.ClientOptions) Client {
		client.options = options
		return client
	}, func(context.Context, ingestion.Delivery) {}, nil)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if err := transport.Start(context.Background()); !errors.Is(err, ErrConnectFailed) {
		t.Fatalf("Start error = %v, want ErrConnectFailed", err)
	}
}

func TestTransportFailsWhenInitialConnectionTimesOut(t *testing.T) {
	client := &fakeClient{connectIncomplete: true}
	config := DefaultConfig("mqtt://127.0.0.1:1883")
	config.ConnectTimeout = 25 * time.Millisecond
	transport, err := New(config, func(options *mqtt.ClientOptions) Client {
		client.options = options
		return client
	}, func(context.Context, ingestion.Delivery) {}, nil)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if err := transport.Start(context.Background()); !errors.Is(err, ErrConnectTimeout) {
		t.Fatalf("Start error = %v, want ErrConnectTimeout", err)
	}
}

func TestTransportFailsWhenInitialSubscriptionNeverBecomesReady(t *testing.T) {
	client := &fakeClient{subscribeErr: errors.New("subscription refused")}
	config := DefaultConfig("mqtt://127.0.0.1:1883")
	config.SubscribeTimeout = 25 * time.Millisecond
	config.SubscribeRetryDelay = time.Millisecond
	transport, err := New(config, func(options *mqtt.ClientOptions) Client {
		client.options = options
		return client
	}, func(context.Context, ingestion.Delivery) {}, nil)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if err := transport.Start(context.Background()); !errors.Is(err, ErrSubscribeTimeout) {
		t.Fatalf("Start error = %v, want ErrSubscribeTimeout", err)
	}
	if !client.disconnected {
		t.Fatal("client was not disconnected after failed initial subscription")
	}
}

func TestTransportDoesNotRestoreReadinessAfterDisconnectBeforeCommit(t *testing.T) {
	client := &fakeClient{}
	client.afterSubscribe = func() { client.Drop() }
	config := DefaultConfig("mqtt://127.0.0.1:1883")
	config.SubscribeTimeout = 25 * time.Millisecond
	config.SubscribeRetryDelay = time.Millisecond
	transport, err := New(config, func(options *mqtt.ClientOptions) Client {
		client.options = options
		return client
	}, func(context.Context, ingestion.Delivery) {}, nil)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if err := transport.Start(context.Background()); !errors.Is(err, ErrSubscribeTimeout) {
		t.Fatalf("Start error = %v, want ErrSubscribeTimeout", err)
	}
	if transport.Ready() {
		t.Fatal("transport became ready after the subscription generation was disconnected")
	}
}

func TestTransportStopHonorsDeadlineWhenUnsubscribeStalls(t *testing.T) {
	unsubscribeBlock := make(chan struct{})
	client := &fakeClient{
		unsubscribeStarted: make(chan struct{}),
		unsubscribeBlock:   unsubscribeBlock,
	}
	transport, err := New(DefaultConfig("mqtt://127.0.0.1:1883"), func(options *mqtt.ClientOptions) Client {
		client.options = options
		return client
	}, func(context.Context, ingestion.Delivery) {}, nil)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	stopContext, cancelStop := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancelStop()
	stopDone := make(chan error, 1)
	go func() { stopDone <- transport.Stop(stopContext) }()
	select {
	case <-client.unsubscribeStarted:
	case <-time.After(time.Second):
		t.Fatal("unsubscribe did not start")
	}
	select {
	case err := <-stopDone:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Stop error = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop did not honor its deadline")
	}
	close(unsubscribeBlock)
}

func TestTransportStartWithCanceledContextDoesNotPoisonStop(t *testing.T) {
	client := &fakeClient{}
	transport, err := New(DefaultConfig("mqtt://127.0.0.1:1883"), func(options *mqtt.ClientOptions) Client {
		client.options = options
		return client
	}, func(context.Context, ingestion.Delivery) {}, nil)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := transport.Start(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Start error = %v, want context.Canceled", err)
	}
	if err := transport.Stop(context.Background()); err != nil {
		t.Fatalf("Stop after canceled Start returned error: %v", err)
	}
}

func TestTransportDrainsQueuedWorkBeforeStopping(t *testing.T) {
	client := &fakeClient{unsubscribeStarted: make(chan struct{})}
	config := DefaultConfig("mqtt://127.0.0.1:1883")
	config.QueueSize = 1
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	processed := make(chan ingestion.Delivery, 2)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	transport, err := New(config, func(options *mqtt.ClientOptions) Client {
		client.options = options
		return client
	}, func(ctx context.Context, delivery ingestion.Delivery) {
		startedOnce.Do(func() { close(started) })
		select {
		case <-release:
			processed <- delivery
		case <-ctx.Done():
		}
	}, logger)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	client.Emit(fakeMessage{payload: []byte("first"), qos: 1})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first delivery did not start")
	}
	client.Emit(fakeMessage{payload: []byte("second"), qos: 1})
	client.Emit(fakeMessage{payload: []byte("dropped"), qos: 1})

	stopContext, cancelStop := context.WithTimeout(context.Background(), time.Second)
	defer cancelStop()
	stopDone := make(chan error, 1)
	go func() { stopDone <- transport.Stop(stopContext) }()
	select {
	case <-client.unsubscribeStarted:
		client.Emit(fakeMessage{payload: []byte("after-drain-start"), qos: 1})
	case <-time.After(time.Second):
		t.Fatal("unsubscribe did not start")
	}
	close(release)
	if err := <-stopDone; err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if len(processed) != 2 {
		t.Fatalf("processed deliveries = %d, want first plus queued second", len(processed))
	}
	if !strings.Contains(logs.String(), "reason_code=ingestion_overloaded") {
		t.Fatalf("transport logs = %q, want queue saturation reason", logs.String())
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition did not become true")
}
