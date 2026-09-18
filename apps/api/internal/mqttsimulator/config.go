// Package mqttsimulator owns the standalone local device transport fixture.
// It deliberately has no dependency on the API, registry, database, or HTTP
// server packages.
package mqttsimulator

import (
	"errors"
	"maps"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

const (
	EnvironmentDevelopment = "development"
	EnvironmentTest        = "test"
	ExpectedTenantSlug     = "pulsegrid-dev"
	DevelopmentBrokerURL   = "mqtt://127.0.0.1:1883"
	TestBrokerURL          = "mqtt://127.0.0.1:11883"

	brokerURLKey   = "PULSEGRID_MQTT_BROKER_URL"
	tenantSlugKey  = "PULSEGRID_MQTT_TENANT_SLUG"
	deviceIDKey    = "PULSEGRID_MQTT_DEVICE_ID"
	temperatureKey = "PULSEGRID_SIMULATOR_TEMPERATURE_CELSIUS"
)

// Config is the complete standalone simulator configuration. It contains no
// API or database settings because the simulator is an external-device-shaped
// process rather than an application client.
type Config struct {
	Environment        string
	BrokerURL          string
	TenantSlug         string
	DeviceID           uuid.UUID
	TemperatureCelsius float64
}

// DotenvReader is injectable so configuration precedence and isolation can be
// tested without reading a contributor's local files.
type DotenvReader func(path string) (map[string]string, error)

// Load reads process configuration and, for development or test only, fills
// missing values from the selected local dotenv file.
func Load() (Config, error) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return Config{}, errors.New("get simulator configuration directory")
	}

	return LoadFrom(environmentFromOS(), workingDirectory, readLocalDotenv)
}

// LoadFrom parses a deterministic environment map. Process values always win
// over the selected local dotenv file, including an explicitly empty value.
func LoadFrom(processEnvironment map[string]string, workingDirectory string, readDotenv DotenvReader) (Config, error) {
	rawEnvironment, ok := processEnvironment["PULSEGRID_ENV"]
	if !ok || strings.TrimSpace(rawEnvironment) == "" {
		return Config{}, errors.New("simulator configuration PULSEGRID_ENV is required")
	}

	environment := strings.ToLower(strings.TrimSpace(rawEnvironment))
	if environment != EnvironmentDevelopment && environment != EnvironmentTest {
		return Config{}, errors.New("simulator configuration PULSEGRID_ENV must be development or test")
	}

	values := cloneValues(processEnvironment)
	if readDotenv == nil {
		readDotenv = readLocalDotenv
	}
	if workingDirectory == "" {
		workingDirectory = "."
	}

	dotenvValues, readErr := readDotenv(filepath.Join(workingDirectory, ".env."+environment))
	if readErr != nil && !os.IsNotExist(readErr) {
		return Config{}, errors.New("load simulator dotenv configuration")
	}
	for key, value := range dotenvValues {
		if _, exists := processEnvironment[key]; !exists {
			values[key] = value
		}
	}

	brokerURL, err := parseBrokerURL(values[brokerURLKey], environment)
	if err != nil {
		return Config{}, err
	}
	if values[tenantSlugKey] != ExpectedTenantSlug {
		return Config{}, errors.New("simulator configuration PULSEGRID_MQTT_TENANT_SLUG must be pulsegrid-dev")
	}

	deviceID, err := parseDeviceID(values[deviceIDKey])
	if err != nil {
		return Config{}, err
	}
	temperature, err := parseTemperature(values[temperatureKey])
	if err != nil {
		return Config{}, err
	}

	return Config{
		Environment:        environment,
		BrokerURL:          brokerURL,
		TenantSlug:         ExpectedTenantSlug,
		DeviceID:           deviceID,
		TemperatureCelsius: temperature,
	}, nil
}

func parseBrokerURL(rawURL string, environment string) (string, error) {
	if rawURL == "" {
		return "", errors.New("simulator configuration PULSEGRID_MQTT_BROKER_URL is required")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "mqtt" || parsed.Hostname() != "127.0.0.1" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return "", errors.New("simulator configuration PULSEGRID_MQTT_BROKER_URL must be a loopback mqtt URL")
	}

	expected := DevelopmentBrokerURL
	if environment == EnvironmentTest {
		expected = TestBrokerURL
	}
	if rawURL != expected {
		return "", errors.New("simulator configuration PULSEGRID_MQTT_BROKER_URL must use the environment-specific loopback broker")
	}

	return rawURL, nil
}

func parseDeviceID(rawID string) (uuid.UUID, error) {
	if rawID == "" {
		return uuid.Nil, errors.New("simulator configuration PULSEGRID_MQTT_DEVICE_ID is required")
	}

	parsed, err := uuid.Parse(rawID)
	if err != nil || parsed == uuid.Nil || parsed.String() != rawID {
		return uuid.Nil, errors.New("simulator configuration PULSEGRID_MQTT_DEVICE_ID must be a canonical lowercase UUID")
	}
	return parsed, nil
}

func parseTemperature(rawTemperature string) (float64, error) {
	if rawTemperature == "" {
		return 0, errors.New("simulator configuration PULSEGRID_SIMULATOR_TEMPERATURE_CELSIUS is required")
	}

	temperature, err := strconv.ParseFloat(rawTemperature, 64)
	if err != nil || math.IsNaN(temperature) || math.IsInf(temperature, 0) {
		return 0, errors.New("simulator configuration PULSEGRID_SIMULATOR_TEMPERATURE_CELSIUS must be a finite number")
	}
	return temperature, nil
}

func readLocalDotenv(path string) (map[string]string, error) {
	return godotenv.Read(path)
}

func environmentFromOS() map[string]string {
	values := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	return values
}

func cloneValues(values map[string]string) map[string]string {
	clone := make(map[string]string, len(values))
	maps.Copy(clone, values)
	return clone
}

// NewClientOptions creates the bounded MQTT 3.1.1 client options used by the
// one-shot publisher. The caller supplies the broker URL only after Config has
// enforced the environment and loopback boundary.
func NewClientOptions(cfg Config, clientID string) *mqtt.ClientOptions {
	return mqtt.NewClientOptions().
		AddBroker(cfg.BrokerURL).
		SetClientID(clientID).
		SetProtocolVersion(4).
		SetCleanSession(true).
		SetOrderMatters(false).
		SetAutoReconnect(false).
		SetConnectRetry(false).
		SetConnectTimeout(connectTimeout).
		SetWriteTimeout(publishTimeout).
		SetKeepAlive(10 * time.Second)
}

const (
	connectTimeout = 5 * time.Second
	publishTimeout = 5 * time.Second
)
