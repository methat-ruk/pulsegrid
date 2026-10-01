package mqttsimulator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/commands/mqttcontract"
)

const (
	commandQueueSize        = 64
	commandConnectTimeout   = 5 * time.Second
	commandSubscribeTimeout = 5 * time.Second
	commandWriteTimeout     = 2 * time.Second
	commandResponseTimeout  = 2 * time.Second
	commandReconnectStart   = 500 * time.Millisecond
	commandReconnectMax     = 5 * time.Second
	maxCommandTopicBytes    = 256
)

var (
	ErrCommandSubscribeTimeout = errors.New("simulator command subscription timed out")
	ErrCommandSubscribeFailed  = errors.New("simulator command subscription failed")
)

type CommandMessage interface {
	Topic() string
	Payload() []byte
	QoS() byte
	Retained() bool
	Duplicate() bool
}

type CommandClient interface {
	Connect() Token
	Subscribe(string, byte, func(CommandMessage)) Token
	Publish(string, byte, bool, any) Token
	Disconnect(uint)
	IsConnectionOpen() bool
}

type CommandClientFactory func(*mqtt.ClientOptions) CommandClient

type commandDelivery struct {
	topic      string
	payload    []byte
	qos        byte
	retained   bool
	duplicate  bool
	epoch      uint64
	oversized  bool
	topicLarge bool
	deliveryID uuid.UUID
}

// RunCommands receives local diagnostic PING commands until ctx is cancelled.
// Its callback only copies bounded metadata; one worker owns decoding/replies.
func RunCommands(ctx context.Context, config Config, factory CommandClientFactory, logger *slog.Logger) error {
	if config.Mode != ModeCommands {
		return errors.New("simulator commands mode is not enabled")
	}
	if factory == nil {
		return errors.New("simulator command MQTT client factory is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if logger == nil {
		logger = slog.Default()
	}
	commandTopic, err := mqttcontract.CommandTopic(config.TenantSlug, config.DeviceID)
	if err != nil {
		return err
	}
	responseTopic, err := mqttcontract.ResponseTopic(config.TenantSlug, config.DeviceID)
	if err != nil {
		return err
	}
	queue := make(chan commandDelivery, commandQueueSize)
	var currentEpoch atomic.Uint64
	clientID := "pulsegrid-device-command-simulator-" + uuid.NewString()
	delay := commandReconnectStart

	for ctx.Err() == nil {
		epoch := currentEpoch.Add(1)
		client := factory(NewCommandClientOptions(config, clientID+"-"+uuid.NewString()))
		if client == nil {
			if !waitReconnect(ctx, delay) {
				return nil
			}
			delay = nextReconnectDelay(delay)
			continue
		}
		if err := waitCommandToken(ctx, client.Connect(), commandConnectTimeout, ErrConnectTimeout, ErrConnectFailed); err != nil {
			client.Disconnect(0)
			if !waitReconnect(ctx, delay) {
				return nil
			}
			delay = nextReconnectDelay(delay)
			logger.Warn("device simulator command connection unavailable", "reason_code", "simulator_command_connect_failed")
			continue
		}
		topicToken := client.Subscribe(commandTopic, 1, func(message CommandMessage) {
			delivery := copyCommandDelivery(message, epoch)
			if delivery == nil || currentEpoch.Load() != epoch {
				return
			}
			select {
			case queue <- *delivery:
			default:
				logger.Warn("device simulator command queue is full", "reason_code", "simulator_command_overloaded", "delivery_id", delivery.deliveryID)
			}
		})
		if err := waitCommandToken(ctx, topicToken, commandSubscribeTimeout, ErrCommandSubscribeTimeout, ErrCommandSubscribeFailed); err == nil &&
			client.IsConnectionOpen() && subscriptionGranted(topicToken) {
			logger.Info("device simulator command subscription ready", "reason_code", "simulator_command_ready", "device_id", config.DeviceID)
			delay = commandReconnectStart
			err = runCommandQueue(ctx, client, epoch, &currentEpoch, config, commandTopic, responseTopic, queue, logger)
			client.Disconnect(500)
			if ctx.Err() != nil {
				return nil
			}
			if err != nil {
				logger.Warn("device simulator command session ended", "reason_code", "simulator_command_session_ended", "device_id", config.DeviceID)
			}
		} else {
			currentEpoch.Add(1)
			client.Disconnect(0)
			logger.Warn("device simulator command subscription unavailable", "reason_code", "simulator_command_subscribe_failed", "device_id", config.DeviceID)
		}
		if !waitReconnect(ctx, delay) {
			return nil
		}
		delay = nextReconnectDelay(delay)
	}
	return nil
}

func runCommandQueue(ctx context.Context, client CommandClient, epoch uint64, currentEpoch *atomic.Uint64, config Config, commandTopic, responseTopic string, queue <-chan commandDelivery, logger *slog.Logger) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			currentEpoch.Add(1)
			return nil
		case <-ticker.C:
			if !client.IsConnectionOpen() {
				currentEpoch.Add(1)
				return errors.New("simulator command broker disconnected")
			}
		case delivery := <-queue:
			if delivery.epoch != epoch {
				continue
			}
			if err := handleCommandDelivery(ctx, client, epoch, currentEpoch, config, commandTopic, responseTopic, delivery, logger); err != nil {
				currentEpoch.Add(1)
				return err
			}
		}
	}
}

func handleCommandDelivery(ctx context.Context, client CommandClient, epoch uint64, currentEpoch *atomic.Uint64, config Config, commandTopic, responseTopic string, delivery commandDelivery, logger *slog.Logger) error {
	if delivery.oversized {
		logger.Warn("device simulator command rejected", "reason_code", "simulator_command_too_large", "delivery_id", delivery.deliveryID)
		return nil
	}
	if delivery.topicLarge || delivery.topic != commandTopic {
		logger.Warn("device simulator command rejected", "reason_code", "simulator_command_topic_invalid", "delivery_id", delivery.deliveryID)
		return nil
	}
	if delivery.qos != 1 || delivery.retained {
		logger.Warn("device simulator command rejected", "reason_code", "simulator_command_transport_invalid", "delivery_id", delivery.deliveryID, "qos", delivery.qos, "retained", delivery.retained)
		return nil
	}
	command, err := mqttcontract.DecodeCommand(delivery.payload)
	if err != nil {
		logger.Warn("device simulator command rejected", "reason_code", "simulator_command_payload_invalid", "delivery_id", delivery.deliveryID, "payload_bytes", len(delivery.payload))
		return nil
	}
	now := time.Now().UTC()
	if command.CreatedAt.After(now) {
		logger.Warn("device simulator command rejected", "reason_code", "simulator_command_time_invalid", "device_id", config.DeviceID, "command_id", command.CommandID)
		return nil
	}
	if !now.Before(command.ExpiresAt) {
		logger.Warn("device simulator command rejected", "reason_code", "simulator_command_expired", "device_id", config.DeviceID, "command_id", command.CommandID)
		return nil
	}
	if config.CommandResponse == ResponseSilent {
		logger.Info("device simulator left command unanswered", "reason_code", "simulator_command_silent", "device_id", config.DeviceID, "command_id", command.CommandID, "duplicate", delivery.duplicate)
		return nil
	}
	if err := publishSimulatorResponse(ctx, client, epoch, currentEpoch, responseTopic, command.CommandID, mqttcontract.OutcomeAck, ""); err != nil {
		return err
	}
	if config.CommandResponse == ResponseAckOnly {
		logger.Info("device simulator acknowledged command", "reason_code", "simulator_command_acknowledged", "device_id", config.DeviceID, "command_id", command.CommandID, "duplicate", delivery.duplicate)
		return nil
	}
	outcome, failure := mqttcontract.OutcomeCompleted, ""
	if config.CommandResponse == ResponseFailure {
		outcome, failure = mqttcontract.OutcomeFailed, mqttcontract.DeviceFailureCode
	}
	if err := publishSimulatorResponse(ctx, client, epoch, currentEpoch, responseTopic, command.CommandID, outcome, failure); err != nil {
		return err
	}
	logger.Info("device simulator completed command response", "reason_code", "simulator_command_responded", "device_id", config.DeviceID, "command_id", command.CommandID, "outcome", outcome, "duplicate", delivery.duplicate)
	return nil
}

func publishSimulatorResponse(ctx context.Context, client CommandClient, epoch uint64, currentEpoch *atomic.Uint64, topic string, commandID uuid.UUID, outcome mqttcontract.Outcome, failureCode string) error {
	if ctx.Err() != nil || currentEpoch.Load() != epoch || !client.IsConnectionOpen() {
		return errors.New("simulator command MQTT connection is unavailable")
	}
	payload, err := mqttcontract.EncodeResponse(commandID, outcome, failureCode)
	if err != nil {
		return err
	}
	token := client.Publish(topic, 1, false, payload)
	return waitCommandToken(ctx, token, commandResponseTimeout, ErrPublishTimeout, ErrPublishFailed)
}

func copyCommandDelivery(message CommandMessage, epoch uint64) *commandDelivery {
	if message == nil {
		return nil
	}
	delivery := &commandDelivery{
		qos: message.QoS(), retained: message.Retained(), duplicate: message.Duplicate(),
		epoch: epoch, deliveryID: uuid.New(),
	}
	topic := message.Topic()
	if len(topic) > maxCommandTopicBytes {
		delivery.topicLarge = true
	} else {
		delivery.topic = string(append([]byte(nil), topic...))
	}
	payload := message.Payload()
	if len(payload) > mqttcontract.MaxPayloadBytes {
		delivery.oversized = true
	} else {
		delivery.payload = append([]byte(nil), payload...)
	}
	return delivery
}

func NewCommandClientOptions(config Config, clientID string) *mqtt.ClientOptions {
	return mqtt.NewClientOptions().
		AddBroker(config.BrokerURL).
		SetClientID(clientID).
		SetProtocolVersion(4).
		SetCleanSession(true).
		SetOrderMatters(false).
		SetAutoReconnect(false).
		SetConnectRetry(false).
		SetConnectTimeout(commandConnectTimeout).
		SetWriteTimeout(commandWriteTimeout).
		SetKeepAlive(10 * time.Second)
}

func PahoCommandClientFactory(options *mqtt.ClientOptions) CommandClient {
	return pahoCommandClient{client: mqtt.NewClient(options)}
}

type pahoCommandClient struct{ client mqtt.Client }

func (c pahoCommandClient) Connect() Token { return pahoToken{token: c.client.Connect()} }

func (c pahoCommandClient) Subscribe(topic string, qos byte, handler func(CommandMessage)) Token {
	return pahoSubscribeToken{token: c.client.Subscribe(topic, qos, func(_ mqtt.Client, message mqtt.Message) {
		handler(pahoCommandMessage{message})
	})}
}

func (c pahoCommandClient) Publish(topic string, qos byte, retained bool, payload any) Token {
	return pahoToken{token: c.client.Publish(topic, qos, retained, payload)}
}

func (c pahoCommandClient) Disconnect(quiesce uint) { c.client.Disconnect(quiesce) }

func (c pahoCommandClient) IsConnectionOpen() bool { return c.client.IsConnectionOpen() }

type pahoSubscribeToken struct{ token mqtt.Token }

func (t pahoSubscribeToken) WaitTimeout(timeout time.Duration) bool {
	return t.token.WaitTimeout(timeout)
}
func (t pahoSubscribeToken) Error() error { return t.token.Error() }
func (t pahoSubscribeToken) Result() map[string]byte {
	if result, ok := t.token.(*mqtt.SubscribeToken); ok {
		return result.Result()
	}
	return nil
}

func subscriptionGranted(token Token) bool {
	subscribeToken, ok := token.(interface{ Result() map[string]byte })
	if !ok {
		return false
	}
	results := subscribeToken.Result()
	if len(results) != 1 {
		return false
	}
	for _, qos := range results {
		return qos == 1
	}
	return false
}

type pahoCommandMessage struct{ message mqtt.Message }

func (m pahoCommandMessage) Topic() string   { return m.message.Topic() }
func (m pahoCommandMessage) Payload() []byte { return m.message.Payload() }
func (m pahoCommandMessage) QoS() byte       { return m.message.Qos() }
func (m pahoCommandMessage) Retained() bool  { return m.message.Retained() }
func (m pahoCommandMessage) Duplicate() bool { return m.message.Duplicate() }

func waitCommandToken(ctx context.Context, token Token, timeout time.Duration, timeoutErr, operationErr error) error {
	if token == nil {
		return operationErr
	}
	wait := timeout
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < wait {
			wait = remaining
		}
	}
	if wait <= 0 || !token.WaitTimeout(wait) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return timeoutErr
	}
	if err := token.Error(); err != nil {
		return fmt.Errorf("%w: %v", operationErr, err)
	}
	return nil
}

func waitReconnect(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func nextReconnectDelay(delay time.Duration) time.Duration {
	if delay >= commandReconnectMax/2 {
		return commandReconnectMax
	}
	return delay * 2
}
