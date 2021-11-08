package main

import (
	"io/ioutil"
	"os"
	"strings"
	"testing"
)

func assertError(t testing.TB, got error, want error) {
	t.Helper()

	if got != want {
		t.Errorf("got error %q, want %q", got, want)
	}
}

func assertErrorMessageSubString(t testing.TB, got error, want string) {
	t.Helper()

	if !strings.Contains(got.Error(), want) {
		t.Errorf("got error %q, want to contain %q", got, want)
	}
}

func assertString(t testing.TB, got string, want string) {
	t.Helper()

	if got != want {
		t.Errorf("got error %s, want %s", got, want)
	}
}

func writeTestLogFile(tmpFile *os.File, content string) error {
	if err := os.WriteFile(tmpFile.Name(), []byte(content), 0666); err != nil {
		return err
	}
	return nil
}

func TestLogFileErrors(t *testing.T) {

	t.Run("no such file", func(t *testing.T) {
		_, err := processLogFile("nofile.txt")
		assertErrorMessageSubString(t, err, ErrOpenFile)
	})
}

func TestReferenceLineErrors(t *testing.T) {

	tmpFile, err := ioutil.TempFile("", "sensors")
	if err != nil {
		t.Error("Error creating test log file")
		return
	}
	defer os.Remove(tmpFile.Name())

	t.Run("wrong reference fields", func(t *testing.T) {
		if err := writeTestLogFile(tmpFile, "reference"); err != nil {
			t.Error("Error writing test log file")
			return
		}
		_, err := processLogFile(tmpFile.Name())
		assertErrorMessageSubString(t, err, ErrWrongNumberRefFields)
	})

	t.Run("wrong temp type", func(t *testing.T) {
		if err := writeTestLogFile(tmpFile, "reference a 2"); err != nil {
			t.Error("Error writing test log file")
			return
		}
		_, err := processLogFile(tmpFile.Name())
		assertErrorMessageSubString(t, err, ErrTempNotFloat)
	})

	t.Run("wrong humidity type", func(t *testing.T) {
		if err := writeTestLogFile(tmpFile, "reference 2 a"); err != nil {
			t.Error("Error writing test log file")
			return
		}
		_, err := processLogFile(tmpFile.Name())
		assertErrorMessageSubString(t, err, ErrHumidityNotFloat)
	})
}

const noSensors = "reference 100 0"
const tempUltraPrecise = `reference 100 0
thermometer temp-1
2007-04-05T22:00 100
2007-04-05T22:01 100.1
2007-04-05T22:02 99.9`
const tempVeryPrecise = `reference 100 0
thermometer temp-1
2007-04-05T22:00 100
2007-04-05T22:01 104
2007-04-05T22:02 96`
const tempPrecise01 = `reference 100 0
thermometer temp-1
2007-04-05T22:00 200
2007-04-05T22:02 0`
const tempPrecise02 = `reference 100 0
thermometer temp-1`

func TestThermometers(t *testing.T) {
	tmpFile, err := ioutil.TempFile("", "sensors")
	if err != nil {
		t.Error("Error creating test log file")
		return
	}
	defer os.Remove(tmpFile.Name())

	t.Run("no sensors", func(t *testing.T) {
		if err := writeTestLogFile(tmpFile, noSensors); err != nil {
			t.Error("Error writing test log file")
			return
		}
		val, err := processLogFile(tmpFile.Name())
		assertError(t, err, nil)
		assertString(t, val, `{}`)
	})

	t.Run("temp ultra precise", func(t *testing.T) {
		if err := writeTestLogFile(tmpFile, tempUltraPrecise); err != nil {
			t.Error("Error writing test log file")
			return
		}
		val, err := processLogFile(tmpFile.Name())
		assertError(t, err, nil)
		assertString(t, val, `{
  "temp-1": "ultra precise"
}`)
	})

	t.Run("temp very precise", func(t *testing.T) {
		if err := writeTestLogFile(tmpFile, tempVeryPrecise); err != nil {
			t.Error("Error writing test log file")
			return
		}
		val, err := processLogFile(tmpFile.Name())
		assertError(t, err, nil)
		assertString(t, val, `{
  "temp-1": "very precise"
}`)
	})

	t.Run("temp precise", func(t *testing.T) {
		if err := writeTestLogFile(tmpFile, tempPrecise01); err != nil {
			t.Error("Error writing test log file")
			return
		}
		val, err := processLogFile(tmpFile.Name())
		assertError(t, err, nil)
		assertString(t, val, `{
  "temp-1": "precise"
}`)
	})

	t.Run("temp precise (no data)", func(t *testing.T) {
		if err := writeTestLogFile(tmpFile, tempPrecise02); err != nil {
			t.Error("Error writing test log file")
			return
		}
		val, err := processLogFile(tmpFile.Name())
		assertError(t, err, nil)
		assertString(t, val, `{
  "temp-1": "precise"
}`)
	})

}

const humSensorKeep01 = `reference 0 45
humidity hum-1
2007 45.2
2007 45.3
2007 45.1`

const humSensorKeep02 = `reference 0 45
humidity hum-1`

const humSensorDiscard01 = `reference 0 45
humidity hum-1
2007 45.5`

func TestHumiditySensors(t *testing.T) {
	tmpFile, err := ioutil.TempFile("", "sensors")
	if err != nil {
		t.Error("Error creating test log file")
		return
	}
	defer os.Remove(tmpFile.Name())

	t.Run("humidity sensor keep", func(t *testing.T) {
		if err := writeTestLogFile(tmpFile, humSensorKeep01); err != nil {
			t.Error("Error writing test log file")
			return
		}
		val, err := processLogFile(tmpFile.Name())
		assertError(t, err, nil)
		assertString(t, val, `{
  "hum-1": "keep"
}`)
	})

	t.Run("humidity sensor keep (no data)", func(t *testing.T) {
		if err := writeTestLogFile(tmpFile, humSensorKeep02); err != nil {
			t.Error("Error writing test log file")
			return
		}
		val, err := processLogFile(tmpFile.Name())
		assertError(t, err, nil)
		assertString(t, val, `{
  "hum-1": "keep"
}`)
	})

	t.Run("humidity sensor discard", func(t *testing.T) {
		if err := writeTestLogFile(tmpFile, humSensorDiscard01); err != nil {
			t.Error("Error writing test log file")
			return
		}
		val, err := processLogFile(tmpFile.Name())
		assertError(t, err, nil)
		assertString(t, val, `{
  "hum-1": "discard"
}`)
	})
}
