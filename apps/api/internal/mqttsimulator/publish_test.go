package mqttsimulator

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/eclipse/paho.mqtt.golang"
)

type fakeToken struct {
	completed bool
	err       error
}

func (t fakeToken) WaitTimeout(time.Duration) bool { return t.completed }
func (t fakeToken) Error() error                   { return t.err }

type fakeClient struct {
	connectToken fakeToken
	publishToken fakeToken
	topic        string
	qos          byte
	retained     bool
	payload      []byte
	disconnected uint
}

func (c *fakeClient) Connect() Token { return c.connectToken }

func (c *fakeClient) Publish(topic string, qos byte, retained bool, payload interface{}) Token {
	c.topic = topic
	c.qos = qos
	c.retained = retained
	c.payload, _ = payload.([]byte)
	return c.publishToken
}

func (c *fakeClient) Disconnect(quiesce uint) { c.disconnected = quiesce }

func TestPublishOnceUsesQoSOneNonRetainedBoundedClient(t *testing.T) {
	cfg, err := LoadFrom(validEnvironment(EnvironmentTest), t.TempDir(), missingDotenv)
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}
	client := &fakeClient{connectToken: fakeToken{completed: true}, publishToken: fakeToken{completed: true}}
	var options *mqtt.ClientOptions
	result, err := PublishOnce(cfg, time.Date(2026, time.September, 18, 4, 0, 0, 0, time.UTC), func(got *mqtt.ClientOptions) Client {
		options = got
		return client
	})
	if err != nil {
		t.Fatalf("PublishOnce returned error: %v", err)
	}
	if options == nil || options.ProtocolVersion != 4 || options.CleanSession != true || options.AutoReconnect || options.ConnectRetry || options.ConnectTimeout != connectTimeout || options.WriteTimeout != publishTimeout {
		t.Fatalf("unexpected MQTT options: %+v", options)
	}
	if client.topic != cfg.Topic() || client.qos != 1 || client.retained || client.disconnected != 1000 {
		t.Fatalf("publish settings = topic %q qos %d retained %t disconnected %d", client.topic, client.qos, client.retained, client.disconnected)
	}
	var payload map[string]any
	if err := json.Unmarshal(client.payload, &payload); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if payload["schemaVersion"] != float64(1) || result.Topic != client.topic {
		t.Fatalf("result/payload = %+v / %s", result, client.payload)
	}
}

func TestPublishOnceReturnsBoundedFailuresAndDisconnects(t *testing.T) {
	cfg, err := LoadFrom(validEnvironment(EnvironmentDevelopment), t.TempDir(), missingDotenv)
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}
	tests := []struct {
		name   string
		client *fakeClient
		want   error
	}{
		{name: "connect timeout", client: &fakeClient{connectToken: fakeToken{}}, want: ErrConnectTimeout},
		{name: "connect failure", client: &fakeClient{connectToken: fakeToken{completed: true, err: errors.New("secret broker detail")}}, want: ErrConnectFailed},
		{name: "publish timeout", client: &fakeClient{connectToken: fakeToken{completed: true}, publishToken: fakeToken{}}, want: ErrPublishTimeout},
		{name: "publish failure", client: &fakeClient{connectToken: fakeToken{completed: true}, publishToken: fakeToken{completed: true, err: errors.New("secret publish detail")}}, want: ErrPublishFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, gotErr := PublishOnce(cfg, time.Now().UTC(), func(*mqtt.ClientOptions) Client { return test.client })
			if !errors.Is(gotErr, test.want) {
				t.Fatalf("error = %v, want %v", gotErr, test.want)
			}
			if test.client.disconnected != 1000 {
				t.Fatalf("disconnect quiesce = %d, want 1000", test.client.disconnected)
			}
		})
	}
}
