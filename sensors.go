package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis"
	"github.com/pkg/errors"
	"gonum.org/v1/gonum/stat"

	"golang.org/x/net/html"
)

const (
	ErrOpenFile                = "error opening file"
	ErrWrongNumberRefFields    = "reference line has incorrect number of fields"
	ErrWrongNumberRedingFields = "line with readings has incorrect number of fields"
	ErrTempNotFloat            = "failed converting reference temperature to float"
	ErrHumidityNotFloat        = "failed converting reference humidity to float"

	ThermometerLabel    = "thermometer"
	HumiditySensorLabel = "humidity"
	ReferenceLabel      = "reference"

	ThermometerUltraPrecise = "ultra precise"
	ThermometerVeryPrecise  = "very precise"
	ThermometerPrecise      = "precise"

	HumiditySensorKeep    = "keep"
	HumiditySensorDiscard = "discard"

	readingLineValues = 2
	outputIndent      = "  "
	logFilePrefix     = "log-"
)

var defaultBranding map[string]string = map[string]string{
	ThermometerLabel:    ThermometerPrecise,
	HumiditySensorLabel: HumiditySensorKeep,
}

type sensor struct {
	branding string
	name     string
}

type thermometer struct {
	sensor
}

type humiditySensor struct {
	sensor
}

type Sensor interface {
	Process(map[string]float64, []float64)
	Name() string
	Branding() string
}

func (s *humiditySensor) Name() string {
	return s.name
}

func (s *humiditySensor) Branding() string {
	return s.branding
}

// Process humidity sensor
// For a humidity sensor, it must be discarded unless it is within 1 humidity percent of the reference value for all readings. (All humidity sensor
// readings are a decimal value representing percent moisture saturation.)
//
// Return value is string of name and branding, already formatted according to the required output format
func (s *humiditySensor) Process(referenceValues map[string]float64, readings []float64) {
	referenceHumidity := referenceValues["Humidity"]
	minHumidity := referenceHumidity - referenceHumidity/100
	maxHumidity := referenceHumidity + referenceHumidity/100

	// Note: going through all readings again is not super efficient (we've already went through them when parsing the file)
	// but having Process method makes the code extensible for future new kind of sensors
	for _, reading := range readings {
		if reading < minHumidity || reading > maxHumidity {
			s.branding = HumiditySensorDiscard
			break
		}
	}
}

func (s *thermometer) Name() string {
	return s.name
}

func (s *thermometer) Branding() string {
	return s.branding
}

// Process thermometer:
// For a thermometer, it is branded “ultra precise” if the mean of the readings is within 0.5 degrees of the known temperature,
// and the standard deviation is less than 3.
// It is branded “very precise” if the mean is within 0.5 degrees of the room, and the standard deviation is under 5.
// Otherwise, it’s sold as “precise”.
//
// Return value is string of name and branding, already formatted according to the required output format
func (s *thermometer) Process(referenceValues map[string]float64, readings []float64) {
	referenceTemperature := referenceValues["Temperature"]

	// we could write the methods for counting mean (trivial) and std deviation (bit more complicated) here,
	// but who could resist the usage of a library...
	mean, std := stat.MeanStdDev(readings, nil)

	if mean > referenceTemperature-0.5 && mean < referenceTemperature+0.5 {
		if std < 3 {
			s.branding = ThermometerUltraPrecise
		} else if std < 5 {
			s.branding = ThermometerVeryPrecise
		}
	}
}

// new sensor factory: return new sensor based on the input type
func NewSensor(sensorType, name string) Sensor {
	if sensorType == ThermometerLabel {
		return &thermometer{
			sensor: sensor{
				name:     name,
				branding: defaultBranding[sensorType],
			},
		}
	} else {
		return &humiditySensor{
			sensor: sensor{
				name:     name,
				branding: defaultBranding[sensorType],
			},
		}
	}
}

// Process the log file with sensor readings, identified by file path.
// Return the text summarizing the branding of sensors mentioned in the log file
func processLogFile(filePath string) (ret string, err error) {
	file, err := os.Open(filePath)
	if err != nil {
		return ret, errors.Wrap(err, ErrOpenFile)
	}
	defer file.Close()

	// Note: if there are more values on reference lines in the future,
	// it might be better to use an array here so we know the values order...
	var referenceValues map[string]float64 = map[string]float64{
		"Temperature": 0.0,
		"Humidity":    0.0,
	}
	var currentReadings []float64 = make([]float64, 0)
	var currentSensor Sensor
	var retMap map[string]string = make(map[string]string)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		l := strings.Split(line, " ")
		switch l[0] {
		case ReferenceLabel:
			if len(l) != len(referenceValues)+1 {
				return ret, errors.New(fmt.Sprintf(ErrWrongNumberRefFields))
			}
			referenceValues["Temperature"], err = strconv.ParseFloat(l[1], 64)
			if err != nil {
				return ret, errors.Wrap(err, ErrTempNotFloat)
			}
			referenceValues["Humidity"], err = strconv.ParseFloat(l[2], 64)
			if err != nil {
				return ret, errors.Wrap(err, ErrHumidityNotFloat)
			}
			for k, v := range referenceValues {
				fmt.Printf("reference value for %s: %.2f\n", k, v)
			}
		case ThermometerLabel, HumiditySensorLabel:
			// hitting the start of some sensor readings: first we must conclude the state
			// of previously processed sensor (if there was any)
			if currentSensor != nil {
				currentSensor.Process(referenceValues, currentReadings)
				retMap[currentSensor.Name()] = currentSensor.Branding()
				// it would make sense to save the _sensor_ branding into DB now
				// (instead of saving log file result)
			}
			// and then create a new one
			currentSensor = NewSensor(l[0], l[1])
			currentReadings = nil
		default:
			if len(l) != readingLineValues {
				return ret, errors.New(fmt.Sprintf(ErrWrongNumberRedingFields))
			}
			reading, err := strconv.ParseFloat(l[1], 64)
			if err != nil {
				return ret, errors.Wrap(err, "failed converting current reading to float")
			}
			currentReadings = append(currentReadings, reading)

		}
	}
	if err := scanner.Err(); err != nil {
		return ret, errors.Wrap(err, "error reading the file")
	}

	// process the last sensor
	if currentSensor != nil {
		currentSensor.Process(referenceValues, currentReadings)
		retMap[currentSensor.Name()] = currentSensor.Branding()
	}

	// is the output format supposed to be a json?
	// Note: when using retMap, we lose the original order of the sensors in the log file ...
	j, _ := json.MarshalIndent(retMap, "", outputIndent)
	ret = string(j)
	return string(j), nil
}

func getRedis() *redis.Client {

	defaultRedisHost := "localhost"
	defaultRedisPort := "6379"

	host, exists := os.LookupEnv("REDIS_HOST")
	if !exists {
		host = defaultRedisHost
	}
	port, exists := os.LookupEnv("REDIS_PORT")
	if !exists {
		port = defaultRedisPort
	}

	fmt.Printf("Connecting to redis host: %s, port %s\n", host, port)

	// TODO set up password too...
	return redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%s", host, port),
		DB:   0,
	})
}

// Helper function to pull the href attribute from a Token
func getHref(t html.Token) (ok bool, href string) {
	for _, a := range t.Attr {
		if a.Key == "href" {
			href = a.Val
			ok = true
		}
	}
	return
}

// Get the URL of a remote directory containing log files.
// Parse the page like an apache directory listing and look for specific pattern
// matching the log files.
// Only return the list of files that were not processed yet.
// Working with assumption that the files are listed from newest to oldest.
func getUprocessedLogFiles(dirURL string, rdb *redis.Client) ([]string, error) {
	ret := make([]string, 0)

	client := &http.Client{}
	req, err := http.NewRequest("GET", dirURL, nil)
	if err != nil {
		return ret, err
	}
	// make sure to close the connection after the request is finished
	req.Close = true

	resp, err := client.Do(req)
	if err != nil {
		return ret, err
	}

	// Note: some retry method would make sense in case of temporary network issues
	// good one is "github.com/hashicorp/go-retryablehttp"

	if err != nil {
		return ret, errors.Wrap(err, "failed to read url "+dirURL)
	}

	body := resp.Body
	defer resp.Body.Close()

	z := html.NewTokenizer(body)

	for {
		tt := z.Next()
		switch {
		case tt == html.ErrorToken:
			return ret, nil
		case tt == html.StartTagToken:
			t := z.Token()
			// Ignore anything but <a> tag
			if t.Data != "a" {
				continue
			}
			// Extract the href value, if there is one
			ok, url := getHref(t)
			if !ok {
				continue
			}
			// Make sure the url begines with right prefix
			if strings.Index(url, logFilePrefix) != 0 {
				continue
			}
			// save only items that are not yet cached in redis
			_, err := rdb.Get(url).Result()
			if err == redis.Nil {
				ret = append(ret, url)
			} else if err != nil {
				return ret, errors.Wrap(err, fmt.Sprintf("Error while fetching %s from redis", url))
			} else {
				// found the first processed file -> exit the scraping method
                                // Note: this only works with the assumption about the way files are sorted!!!
				return ret, nil
			}
		}
	}
}

// downloads the given url as a file with "name" under "directory"
func DownloadFile(url, name, directory string) error {

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound {
		return errors.New(url + " not found")
	}
	defer resp.Body.Close()

	out, err := os.Create(path.Join(directory, name))
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// from the list of log files, find the oldest one not yet processed
func findOldestLogFile(logFiles []string, rdb *redis.Client) (string, error) {

	fileName := ""
	// we just need to process the list of log files with reverse order
	for i := len(logFiles) - 1; i >= 0; i-- {
		logFile := logFiles[i]
		_, err := rdb.Get(logFile).Result()
		if err == redis.Nil {
			fileName = logFile
			break
		} else if err != nil {
			return "", errors.Wrap(err, fmt.Sprintf("Error while fetching %s from redis", logFile))
		}
	}
	return fileName, nil
}

// Fetch the file from remote location and return full path to downloaded file
func fetchLogFile(logFile, dirURL, tmpDir string) (string, error) {

	u, err := url.Parse(dirURL + "/")
	if err != nil {
		return "", errors.Wrap(err, "Failed parsing URL")
	}
	u, err = u.Parse(logFile)
	if err != nil {
		return "", errors.Wrap(err, "Failed parsing URL")
	}
	if err := DownloadFile(u.String(), logFile, tmpDir); err != nil {
		return "", errors.Wrap(err, fmt.Sprintf("Failed downloading remote file %s", u.String()))
	}
	return filepath.Join(tmpDir, logFile), nil
}

func main() {

	tmpDir, err := ioutil.TempDir("", "sensor-logs")
	if err != nil {
		fmt.Printf("Error while creating temp directory: %s\n", err.Error())
		return
	}
	defer os.RemoveAll(tmpDir)

	// Use redis for storing the output and checking if given file was already processed
	// NOTE better design would use some locking to prevent processing the same file by multiple workers
	// e.g. https://github.com/bsm/redislock
	rdb := getRedis()
	_, err = rdb.Ping().Result()
	if err != nil {
		fmt.Printf("Error connecting to REDIS: %s\n", err.Error())
		return
	}

	remoteDir, exists := os.LookupEnv("REMOTE_LOGS_DIR")
	if !exists {
		fmt.Println("Remote directory with log files not provided!")
		return
	}

	// Note: main loop is missing some health check method...
	// (probably by running http server via goroutine)
	for {
		time.Sleep(10 * time.Second)
		logFiles, err := getUprocessedLogFiles(remoteDir, rdb)
		if err != nil {
			fmt.Printf("Error fetching log files: %s\n", err.Error())
			return
		}
		fmt.Printf("got log files: %v\n", logFiles)
		if len(logFiles) == 0 {
			fmt.Println("no new log files")
			time.Sleep(10 * time.Second)
			continue
		}

		fileName, err := findOldestLogFile(logFiles, rdb)
		if err != nil {
			fmt.Printf("Failed checking available the log files: %s\n", err.Error())
			return
		}
		if fileName == "" {
			fmt.Println("no new log file")
			time.Sleep(10 * time.Second)
			continue
		}
		filePath, err := fetchLogFile(fileName, remoteDir, tmpDir)
		if err != nil {
			fmt.Printf("Failed fetching latest log file: %s\n", err.Error())
			return
		}

		processed, err := processLogFile(filePath)

		if err != nil {
			fmt.Printf("Error processing log file: %s\n", err.Error())
			// should we exit now or just proceed with next one?
			// actually let's write the error, otherwise we'll loop on this one forever
			rdb.Set(fileName, err.Error(), 0)
		} else {
			rdb.Set(fileName, processed, 0)
			fmt.Println(processed)
		}
	}
}
