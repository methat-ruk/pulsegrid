package mqttsimulator

import (
	"errors"
	"time"

	"github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
)

var (
	ErrConnectTimeout = errors.New("mqtt connection timed out")
	ErrConnectFailed  = errors.New("mqtt connection failed")
	ErrPublishTimeout = errors.New("mqtt publish timed out")
	ErrPublishFailed  = errors.New("mqtt publish failed")
)

// Token is the bounded completion handle returned by an MQTT operation.
type Token interface {
	WaitTimeout(time.Duration) bool
	Error() error
}

// Client is the small portion of a Paho client used by this one-shot fixture.
// Keeping this interface local makes network failure behavior unit-testable.
type Client interface {
	Connect() Token
	Publish(topic string, qos byte, retained bool, payload interface{}) Token
	Disconnect(quiesce uint)
}

// ClientFactory permits deterministic tests without importing any application
// boundary or starting a broker.
type ClientFactory func(options *mqtt.ClientOptions) Client

// PublishResult contains only the public fixture evidence printed by the
// command; the payload itself is intentionally not logged.
type PublishResult struct {
	Telemetry Telemetry
	Topic     string
}

// PublishOnce connects, publishes one QoS 1 non-retained message, waits for
// the broker acknowledgement, and performs a bounded disconnect.
func PublishOnce(cfg Config, observedAt time.Time, factory ClientFactory) (PublishResult, error) {
	if factory == nil {
		return PublishResult{}, errors.New("mqtt client factory is required")
	}

	topic := cfg.Topic()
	if err := validateTopic(topic); err != nil {
		return PublishResult{}, err
	}
	telemetry := NewTelemetry(observedAt, cfg.TemperatureCelsius)
	payload, err := EncodeTelemetry(telemetry)
	if err != nil {
		return PublishResult{}, err
	}

	clientID := "pulsegrid-device-simulator-" + uuid.NewString()
	client := factory(NewClientOptions(cfg, clientID))
	if client == nil {
		return PublishResult{}, errors.New("mqtt client initialization failed")
	}

	connectToken := client.Connect()
	defer client.Disconnect(1000)
	if !connectToken.WaitTimeout(connectTimeout) {
		return PublishResult{}, ErrConnectTimeout
	}
	if connectToken.Error() != nil {
		return PublishResult{}, ErrConnectFailed
	}

	publishToken := client.Publish(topic, 1, false, payload)
	if !publishToken.WaitTimeout(publishTimeout) {
		return PublishResult{}, ErrPublishTimeout
	}
	if publishToken.Error() != nil {
		return PublishResult{}, ErrPublishFailed
	}

	return PublishResult{Telemetry: telemetry, Topic: topic}, nil
}

// PahoClientFactory adapts the concrete Paho client to the narrow fixture
// interface. The adapter stays in this package and does not cross into API
// application code.
func PahoClientFactory(options *mqtt.ClientOptions) Client {
	return pahoClient{client: mqtt.NewClient(options)}
}

type pahoClient struct {
	client mqtt.Client
}

func (c pahoClient) Connect() Token {
	return pahoToken{token: c.client.Connect()}
}

func (c pahoClient) Publish(topic string, qos byte, retained bool, payload interface{}) Token {
	return pahoToken{token: c.client.Publish(topic, qos, retained, payload)}
}

func (c pahoClient) Disconnect(quiesce uint) {
	c.client.Disconnect(quiesce)
}

type pahoToken struct {
	token mqtt.Token
}

func (t pahoToken) WaitTimeout(timeout time.Duration) bool {
	return t.token.WaitTimeout(timeout)
}

func (t pahoToken) Error() error {
	return t.token.Error()
}
