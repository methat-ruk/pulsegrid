package mqtttransport

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/telemetry/ingestion"
)

type fakeToken struct {
	err      error
	complete bool
}

func (t fakeToken) WaitTimeout(time.Duration) bool { return t.complete }
func (t fakeToken) Error() error                   { return t.err }

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

	mu                sync.Mutex
	connected         bool
	handler           func(Message)
	connectErr        error
	subscribeErr      error
	connectIncomplete bool
	disconnected      bool
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
	defer c.mu.Unlock()
	if c.subscribeErr != nil {
		return fakeToken{complete: true, err: c.subscribeErr}
	}
	c.handler = handler
	return fakeToken{complete: true}
}

func (c *fakeClient) Unsubscribe(...string) Token {
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
	client := &fakeClient{}
	config := DefaultConfig("mqtt://127.0.0.1:1883")
	config.QueueSize = 1
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	processed := make(chan ingestion.Delivery, 2)
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
	}, nil)
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
	close(release)
	if err := <-stopDone; err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if len(processed) != 2 {
		t.Fatalf("processed deliveries = %d, want first plus queued second", len(processed))
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
