package mqtttransport

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eclipse/paho.mqtt.golang"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/commands/mqttcontract"
)

type fakeToken struct {
	wait bool
	err  error
}

func (t fakeToken) WaitTimeout(time.Duration) bool { return t.wait }
func (t fakeToken) Error() error                   { return t.err }

type fakeSubscribeToken struct {
	fakeToken
	grants map[string]byte
}

func (t fakeSubscribeToken) Result() map[string]byte { return t.grants }

type fakeMessage struct {
	topic    string
	payload  []byte
	qos      byte
	retained bool
	dup      bool
}

func (m *fakeMessage) Topic() string   { return m.topic }
func (m *fakeMessage) Payload() []byte { return m.payload }
func (m *fakeMessage) QoS() byte       { return m.qos }
func (m *fakeMessage) Retained() bool  { return m.retained }
func (m *fakeMessage) Duplicate() bool { return m.dup }

type fakeClient struct {
	mu             sync.Mutex
	connected      bool
	connect        Token
	subscribe      SubscribeToken
	publish        Token
	subscribeTopic string
	handler        func(Message)
	publishTopic   string
	publishQoS     byte
	publishRetain  bool
	publishPayload []byte
	disconnects    int
}

func (c *fakeClient) Connect() Token {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.connect == nil {
		c.connected = true
		return fakeToken{wait: true}
	}
	if c.connect.WaitTimeout(time.Second) && c.connect.Error() == nil {
		c.connected = true
	}
	return c.connect
}

func (c *fakeClient) Subscribe(topic string, _ byte, handler func(Message)) SubscribeToken {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subscribeTopic, c.handler = topic, handler
	if c.subscribe == nil {
		return fakeSubscribeToken{fakeToken: fakeToken{wait: true}, grants: map[string]byte{topic: 1}}
	}
	return c.subscribe
}

func (c *fakeClient) Publish(topic string, qos byte, retained bool, payload any) Token {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.publishTopic, c.publishQoS, c.publishRetain = topic, qos, retained
	if bytes, ok := payload.([]byte); ok {
		c.publishPayload = append([]byte(nil), bytes...)
	}
	if c.publish == nil {
		return fakeToken{wait: true}
	}
	return c.publish
}

func (c *fakeClient) Disconnect(uint) {
	c.mu.Lock()
	c.connected = false
	c.disconnects++
	c.mu.Unlock()
}

func (c *fakeClient) IsConnectionOpen() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

func TestTransportSubscribesCopiesBoundedDeliveryAndPublishesQoSOneNonRetained(t *testing.T) {
	client := &fakeClient{}
	transport, err := New(DefaultConfig("mqtt://127.0.0.1:1883"), func(*mqtt.ClientOptions) Client { return client }, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := transport.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Start: %v", err)
	}
	if !transport.Ready() || client.subscribeTopic != "pulsegrid/v1/tenants/pulsegrid-dev/devices/+/command-responses" {
		t.Fatalf("subscription/readiness = (%q, %t)", client.subscribeTopic, transport.Ready())
	}
	topic := "pulsegrid/v1/tenants/pulsegrid-dev/devices/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/command-responses"
	payload := []byte(`{"schemaVersion":1}`)
	client.handler(&fakeMessage{topic: topic, payload: payload, qos: 1})
	payload[0] = 'x'
	select {
	case delivery := <-transport.Deliveries():
		if delivery.Topic != topic || string(delivery.Payload) != `{"schemaVersion":1}` || delivery.PayloadBytes != len(delivery.Payload) {
			t.Fatalf("delivery was not copied: %+v", delivery)
		}
	case <-time.After(time.Second):
		t.Fatal("response was not admitted")
	}
	commandPayload := []byte(`{"schemaVersion":1}`)
	if err := transport.Publish(context.Background(), "pulsegrid/v1/tenants/pulsegrid-dev/devices/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/commands", commandPayload); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if client.publishQoS != 1 || client.publishRetain || string(client.publishPayload) != string(commandPayload) {
		t.Fatalf("publish settings = qos:%d retain:%t payload:%s", client.publishQoS, client.publishRetain, client.publishPayload)
	}
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	cancel()
	if err := transport.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestTransportRejectsSubscriptionWithoutQoSOneGrant(t *testing.T) {
	client := &fakeClient{subscribe: fakeSubscribeToken{fakeToken: fakeToken{wait: true}, grants: map[string]byte{"pulsegrid/v1/tenants/pulsegrid-dev/devices/+/command-responses": 0}}}
	transport, err := New(DefaultConfig("mqtt://127.0.0.1:1883"), func(*mqtt.ClientOptions) Client { return client }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.Start(context.Background()); !errors.Is(err, ErrSubscribeFailed) {
		t.Fatalf("Start error = %v, want ErrSubscribeFailed", err)
	}
	if transport.Ready() || client.disconnects == 0 {
		t.Fatalf("failed subscription left ready client: ready=%t disconnects=%d", transport.Ready(), client.disconnects)
	}
}

func TestTransportBoundsQueueAndMarksOversizedMetadata(t *testing.T) {
	client := &fakeClient{}
	config := DefaultConfig("mqtt://127.0.0.1:1883")
	config.QueueSize = 1
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	transport, err := New(config, func(*mqtt.ClientOptions) Client { return client }, logger)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := transport.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		cancel()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
		defer stopCancel()
		if err := transport.Stop(stopCtx); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}()
	first := &fakeMessage{topic: "first", payload: []byte("first"), qos: 1}
	second := &fakeMessage{topic: "second", payload: []byte("second"), qos: 1}
	client.handler(first)
	client.handler(second)
	if got := <-transport.Deliveries(); string(got.Payload) != "first" {
		t.Fatalf("queue retained %q, want first delivery", got.Payload)
	}
	if len(transport.Deliveries()) != 0 {
		t.Fatal("queue exceeded its one-item bound")
	}

	client.handler(&fakeMessage{topic: "oversized-payload", payload: []byte(strings.Repeat("x", mqttcontract.MaxPayloadBytes+1)), qos: 1})
	if got := <-transport.Deliveries(); !got.Oversized || got.PayloadBytes != mqttcontract.MaxPayloadBytes+1 || len(got.Payload) != 0 {
		t.Fatalf("oversized payload metadata = %+v", got)
	}
	client.handler(&fakeMessage{topic: strings.Repeat("t", maximumTopicBytes+1), payload: []byte("{}"), qos: 1})
	if got := <-transport.Deliveries(); !got.TopicInvalid || got.Topic != "" {
		t.Fatalf("oversized topic metadata = %+v", got)
	}
}

func TestTransportRetiresUncertainPublishAndReconnects(t *testing.T) {
	first := &fakeClient{publish: fakeToken{wait: false}}
	second := &fakeClient{}
	var mu sync.Mutex
	clients := []*fakeClient{first, second}
	factory := func(*mqtt.ClientOptions) Client {
		mu.Lock()
		defer mu.Unlock()
		if len(clients) == 0 {
			return &fakeClient{}
		}
		client := clients[0]
		clients = clients[1:]
		return client
	}
	config := DefaultConfig("mqtt://127.0.0.1:1883")
	config.ReconnectDelay = time.Millisecond
	config.MaximumReconnectDelay = 5 * time.Millisecond
	transport, err := New(config, factory, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if err := transport.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	firstGeneration := transport.Generation()
	if err := transport.Publish(context.Background(), "pulsegrid/v1/tenants/pulsegrid-dev/devices/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/commands", []byte("{}")); !errors.Is(err, ErrPublishTimeout) {
		t.Fatalf("uncertain publish error = %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if transport.Ready() && transport.Generation() > firstGeneration {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
			defer stopCancel()
			if err := transport.Stop(stopCtx); err != nil {
				t.Fatalf("Stop: %v", err)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("transport did not reconnect after uncertain publish: ready=%t generation=%d", transport.Ready(), transport.Generation())
}
