package main

import (
	"fmt"
	"os"
	"time"

	"github.com/methat-ruk/pulsegrid/apps/api/internal/mqttsimulator"
)

func main() {
	cfg, err := mqttsimulator.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %s\n", err)
		os.Exit(1)
	}

	result, err := mqttsimulator.PublishOnce(cfg, time.Now().UTC(), mqttsimulator.PahoClientFactory)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", mqttsimulatorError(err))
		os.Exit(1)
	}

	fmt.Printf("published MQTT telemetry topic=%s message_id=%s observed_at=%s qos=1 retain=false\n", result.Topic, result.Telemetry.MessageID, result.Telemetry.ObservedAt.Format(time.RFC3339Nano))
}

func mqttsimulatorError(err error) string {
	return fmt.Sprintf("device simulator failed: %s", err)
}
