package mqtttransport

import (
	"time"

	"github.com/eclipse/paho.mqtt.golang"
)

// PahoClientFactory adapts the selected Paho MQTT client to the local
// transport seam. Paho types do not cross into ingestion or application code.
func PahoClientFactory(options *mqtt.ClientOptions) Client {
	return pahoClient{client: mqtt.NewClient(options)}
}

type pahoClient struct {
	client mqtt.Client
}

func (c pahoClient) Connect() Token {
	return pahoToken{token: c.client.Connect()}
}

func (c pahoClient) Subscribe(topic string, qos byte, handler func(Message)) Token {
	return pahoToken{token: c.client.Subscribe(topic, qos, func(_ mqtt.Client, message mqtt.Message) {
		handler(pahoMessage{message: message})
	})}
}

func (c pahoClient) Unsubscribe(topics ...string) Token {
	return pahoToken{token: c.client.Unsubscribe(topics...)}
}

func (c pahoClient) Disconnect(quiesce uint) {
	c.client.Disconnect(quiesce)
}

func (c pahoClient) IsConnected() bool {
	return c.client.IsConnected()
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

type pahoMessage struct {
	message mqtt.Message
}

func (m pahoMessage) Topic() string   { return m.message.Topic() }
func (m pahoMessage) Payload() []byte { return m.message.Payload() }
func (m pahoMessage) QoS() byte       { return m.message.Qos() }
func (m pahoMessage) Retained() bool  { return m.message.Retained() }
func (m pahoMessage) Duplicate() bool { return m.message.Duplicate() }
