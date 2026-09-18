package mqttsimulator

import (
	"os"
	"strings"
	"testing"
)

const testDeviceID = "5edacace-70a7-4a8f-846d-3f4d60c56f3a"

func validEnvironment(environment string) map[string]string {
	brokerURL := DevelopmentBrokerURL
	if environment == EnvironmentTest {
		brokerURL = TestBrokerURL
	}
	return map[string]string{
		"PULSEGRID_ENV": environment,
		brokerURLKey:    brokerURL,
		tenantSlugKey:   ExpectedTenantSlug,
		deviceIDKey:     testDeviceID,
		temperatureKey:  "23.5",
	}
}

func TestLoadFromAcceptsEnvironmentSpecificLoopbackConfiguration(t *testing.T) {
	for _, environment := range []string{EnvironmentDevelopment, EnvironmentTest} {
		t.Run(environment, func(t *testing.T) {
			got, err := LoadFrom(validEnvironment(environment), t.TempDir(), missingDotenv)
			if err != nil {
				t.Fatalf("LoadFrom returned error: %v", err)
			}
			if got.Environment != environment || got.BrokerURL != validEnvironment(environment)[brokerURLKey] {
				t.Fatalf("configuration = %+v", got)
			}
			if got.TenantSlug != ExpectedTenantSlug || got.DeviceID.String() != testDeviceID || got.TemperatureCelsius != 23.5 {
				t.Fatalf("configuration values = %+v", got)
			}
		})
	}
}

func TestLoadFromProcessValuesOverrideDotenv(t *testing.T) {
	process := validEnvironment(EnvironmentDevelopment)
	process[temperatureKey] = "24.25"
	got, err := LoadFrom(process, "/tmp/simulator", func(string) (map[string]string, error) {
		return map[string]string{
			temperatureKey: "18.0",
			deviceIDKey:    "00000000-0000-4000-8000-000000000001",
		}, nil
	})
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}
	if got.TemperatureCelsius != 24.25 || got.DeviceID.String() != testDeviceID {
		t.Fatalf("process precedence was not preserved: %+v", got)
	}
}

func TestLoadFromReadsSelectedDotenv(t *testing.T) {
	values := validEnvironment(EnvironmentTest)
	delete(values, brokerURLKey)
	delete(values, tenantSlugKey)
	delete(values, deviceIDKey)
	delete(values, temperatureKey)
	calledPath := ""
	got, err := LoadFrom(values, "/tmp/simulator", func(path string) (map[string]string, error) {
		calledPath = path
		return validEnvironment(EnvironmentTest), nil
	})
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}
	if calledPath != "/tmp/simulator/.env.test" {
		t.Fatalf("dotenv path = %q", calledPath)
	}
	if got.BrokerURL != TestBrokerURL || got.DeviceID.String() != testDeviceID {
		t.Fatalf("dotenv configuration = %+v", got)
	}
}

func TestLoadFromRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(map[string]string)
		want       string
		mustNotLog string
	}{
		{name: "production", mutate: func(values map[string]string) { values["PULSEGRID_ENV"] = "production" }, want: "PULSEGRID_ENV must be development or test"},
		{name: "missing broker", mutate: func(values map[string]string) { values[brokerURLKey] = "" }, want: brokerURLKey + " is required"},
		{name: "non loopback broker", mutate: func(values map[string]string) { values[brokerURLKey] = "mqtt://192.0.2.10:1883" }, want: "loopback mqtt URL", mustNotLog: "192.0.2.10"},
		{name: "unsupported scheme", mutate: func(values map[string]string) { values[brokerURLKey] = "mqtts://127.0.0.1:1883" }, want: "loopback mqtt URL"},
		{name: "userinfo", mutate: func(values map[string]string) { values[brokerURLKey] = "mqtt://user:password@127.0.0.1:1883" }, want: "loopback mqtt URL", mustNotLog: "password"},
		{name: "path", mutate: func(values map[string]string) { values[brokerURLKey] = "mqtt://127.0.0.1:1883/path" }, want: "loopback mqtt URL"},
		{name: "query", mutate: func(values map[string]string) { values[brokerURLKey] = "mqtt://127.0.0.1:1883?secret=1" }, want: "loopback mqtt URL"},
		{name: "wrong test port", mutate: func(values map[string]string) {
			values["PULSEGRID_ENV"] = EnvironmentTest
			values[brokerURLKey] = DevelopmentBrokerURL
		}, want: "environment-specific loopback broker"},
		{name: "wrong tenant", mutate: func(values map[string]string) { values[tenantSlugKey] = "other-tenant" }, want: "TENANT_SLUG must be pulsegrid-dev"},
		{name: "uppercase uuid", mutate: func(values map[string]string) { values[deviceIDKey] = strings.ToUpper(testDeviceID) }, want: "canonical lowercase UUID"},
		{name: "nil uuid", mutate: func(values map[string]string) { values[deviceIDKey] = "00000000-0000-0000-0000-000000000000" }, want: "canonical lowercase UUID"},
		{name: "non finite temperature", mutate: func(values map[string]string) { values[temperatureKey] = "NaN" }, want: "finite number"},
		{name: "overflow temperature", mutate: func(values map[string]string) { values[temperatureKey] = "1e309" }, want: "finite number"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := validEnvironment(EnvironmentDevelopment)
			test.mutate(values)
			_, err := LoadFrom(values, t.TempDir(), missingDotenv)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
			if test.mustNotLog != "" && strings.Contains(err.Error(), test.mustNotLog) {
				t.Fatalf("error exposed sensitive input %q: %v", test.mustNotLog, err)
			}
		})
	}
}

func missingDotenv(string) (map[string]string, error) {
	return nil, os.ErrNotExist
}
