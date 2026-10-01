package mqttsimulator

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/eclipse/paho.mqtt.golang"
)

type cancellingCommandToken struct{ cancel context.CancelFunc }

func (token cancellingCommandToken) WaitTimeout(time.Duration) bool {
	token.cancel()
	return false
}
func (cancellingCommandToken) Error() error { return nil }

type cancellingCommandClient struct {
	cancel       context.CancelFunc
	disconnected bool
}

func (client *cancellingCommandClient) Connect() Token {
	return cancellingCommandToken{cancel: client.cancel}
}
func (*cancellingCommandClient) Subscribe(string, byte, func(CommandMessage)) Token { return nil }
func (*cancellingCommandClient) Publish(string, byte, bool, any) Token              { return nil }
func (client *cancellingCommandClient) Disconnect(uint)                             { client.disconnected = true }
func (*cancellingCommandClient) IsConnectionOpen() bool                             { return false }

func TestRunCommandsTreatsSignalDuringConnectAsCleanShutdown(t *testing.T) {
	values := validEnvironment(EnvironmentTest)
	delete(values, temperatureKey)
	values[simulatorModeKey] = string(ModeCommands)
	config, err := LoadFrom(values, t.TempDir(), missingDotenv)
	if err != nil {
		t.Fatalf("load commands config: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	client := &cancellingCommandClient{cancel: cancel}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err = RunCommands(ctx, config, func(*mqtt.ClientOptions) CommandClient { return client }, logger)
	if err != nil {
		t.Fatalf("RunCommands returned error after shutdown signal: %v", err)
	}
	if !client.disconnected {
		t.Fatal("MQTT client was not disconnected after shutdown signal")
	}
}

func TestRunCommandsRejectsUnsupportedMode(t *testing.T) {
	config, err := LoadFrom(validEnvironment(EnvironmentTest), t.TempDir(), missingDotenv)
	if err != nil {
		t.Fatalf("load telemetry config: %v", err)
	}
	if err := RunCommands(context.Background(), config, func(*mqtt.ClientOptions) CommandClient { return nil }, nil); err == nil {
		t.Fatal("RunCommands accepted telemetry mode")
	} else if err.Error() != "simulator commands mode is not enabled" {
		t.Fatalf("unsupported mode error = %v", err)
	}
}
