package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

/*
{
    "total": 1,                                  "// The total number of results"
    "pageNumber": 1,                             "// The returned page number"
    "result": "OK",                              "// The status of the request"
    "pageSize": 10,                              "// The page size"
    "smshistory": [                              "// The SMS history"
        {
            "order_id" : "XYZABCQWERTY",         "// The order ID"
            "create_time" : "yyyyMMddHHmmss",    "// When the order was created"
            "schedule_time" : "yyyyMMddHHmmss",  "// When the sending is scheduled"
            "message_type" : "GP",               "// The message type"
            "sender" : "MySender",               "// The sender's alias"
            "num_recipients" : 2                 "// The number of recipients"
        },
        {
            ...
        }
    ]
}
*/
type SMSHistory struct {
	Total      int    `json:total`
	PageNumber int    `json:pageNumber`
	Result     string `json:result`
	PageSize   int    `json:pageSize`
	SmsHistory []struct {
		OrderID      string `json:order_id`
		CreateTime   string `json:create_time`
		ScheduleTime string `json:schedule_time`
		MessageType  string `json:message_type`
		Sender       string `json:sender`
		NumRecipient string `json:num_recipient`
	} `json:smshistory`
}

/*
{
    "money": 921.9,
    "sms": [

        {
            "type": "L",
            "quantity": 11815
        },
        {
            "type": "N",
            "quantity": 10407
        },
        {
            "type": "EE",
            "quantity": 10387
        }
    ],
    "email": {
        "bandwidth": 2000.0,
        "purchased": "2015-01-16",
        "billing": "EMAILPERHOUR",
        "expiry": "2016-01-17"
    }
}
*/
type SMSCredit struct {
	Money float64 `json:"money"`
	Sms   []struct {
		Type     string `json:"type"`
		Quantity int    `json:"quantity"`
	} `json:sms`
	Email []struct {
		BandWidth float64 `json:"bandwidth"`
		Purchased string  `json:"purchased"`
		Billing   string  `json:"billing"`
		Expiry    string  `json:"expiry"`
	} `json:"email"`
}

const namespace = "mobyt"
const login_uri = "/API/v1.0/REST/login"
const status_uri = "/API/v1.0/REST/status"
const history_uri = "/API/v1.0/REST/smshistory"

var (
	tr = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client = &http.Client{Transport: tr}

	listenAddress = flag.String("web.listen-address", ":9141",
		"Address to listen on for telemetry")
	metricsPath = flag.String("web.telemetry-path", "/metrics",
		"Path under which to expose metrics")
	configPath = flag.String("config.file-path", "",
		"Path to environment file")

	// Metrics
	up = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "up"),
		"Was the last Mobyt query successful.",
		nil, nil,
	)
	smsSent = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "sms_sent"),
		"Number of sms sent since one hour.",
		[]string{"sender"}, nil,
	)
	smsMoney = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "sms_money"),
		"Account current money",
		[]string{"sender"}, nil,
	)
	smsCredit = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "sms_credit"),
		"Number of remaining sms.",
		[]string{"type"}, nil,
	)
)

func mobytRequest(endpoint string, auth []string) []byte {

	//req := new(http.Request)
	req, err := http.NewRequest(http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		log.Fatalln(err)
	}

	// TODO add check len
	req.Header.Set("user_key", auth[0])
	req.Header.Set("session_key", auth[1])

	log.Printf("Requesting %s", endpoint)
	res, err := client.Do(req)
	if err != nil {
		log.Fatalln(err)
	}

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		log.Fatalln(err)
	}
	//fmt.Println(string(body))
	return body
}

func getSMSLastHourSent(body []byte) int {
	var sms_sent SMSHistory
	if err := json.Unmarshal(body, &sms_sent); err != nil {
		log.Fatalln(err)
	}

	return sms_sent.Total
}

func getSMSCredit(body []byte) (float64, int) {
	var sms_credit SMSCredit
	if err := json.Unmarshal(body, &sms_credit); err != nil {
		log.Fatalln(err)
	}

	return sms_credit.Money, sms_credit.Sms[0].Quantity
}

type Exporter struct {
	mobytEndpoint, mobytUsername, mobytPassword string
}

func NewExporter(mobytEndpoint string, mobytUsername string, mobytPassword string) *Exporter {
	return &Exporter{
		mobytEndpoint: mobytEndpoint,
		mobytUsername: mobytUsername,
		mobytPassword: mobytPassword,
	}
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- up
	ch <- smsSent
	ch <- smsMoney
	ch <- smsCredit
}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	mobytIdSessionKey, err := e.LoadMobytIdSessionMap()
	if err != nil {
		ch <- prometheus.MustNewConstMetric(
			up, prometheus.GaugeValue, 0,
		)
		log.Println(err)
		return
	}
	ch <- prometheus.MustNewConstMetric(
		up, prometheus.GaugeValue, 1,
	)

	e.HitMobytRestApisAndUpdateMetrics(mobytIdSessionKey, ch)
}

func (e *Exporter) LoadMobytIdSessionMap() ([]string, error) {

	req, err := http.NewRequest("GET", e.mobytEndpoint+login_uri, nil)
	if err != nil {
		return nil, err
	}

	// This one line implements the authentication required for the task.
	req.SetBasicAuth(e.mobytUsername, e.mobytPassword)
	// Make request and show output.
	log.Printf("Getting session authentification")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	// fmt.Println(string(body))

	// we initialize our array
	informationKey := strings.Split(string(body), ";")
	if len(informationKey) != 2 {
		return nil, errors.New("Could not get information key")
	}
	log.Printf("Session authentication: Ok")

	return informationKey, nil
}

func (e *Exporter) HitMobytRestApisAndUpdateMetrics(auth []string, ch chan<- prometheus.Metric) {
	// Get SMS Credits
	log.Printf("Get sms credit")
	url := e.mobytEndpoint + status_uri + "?getMoney=true&typeAliases=true"
	sms_money, sms_credit := getSMSCredit(mobytRequest(url, auth))
	//fmt.Println("sms_credit")
	ch <- prometheus.MustNewConstMetric(
		smsCredit, prometheus.GaugeValue, float64(sms_credit), "",
	)
	//fmt.Println("sms_money")
	ch <- prometheus.MustNewConstMetric(
		smsMoney, prometheus.GaugeValue, sms_money, "",
	)

	// Get
	log.Printf("Get one hour sms history")
	current_time := time.Now()
	// define the mobyt time zone
	location, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		log.Fatalln(err)
	}
	localtime := current_time.In(location)
	one_hour_ago := localtime.Add(-1 * time.Hour)
	date := fmt.Sprintf("%d%02d%02d%02d%02d%02d", one_hour_ago.Year(), one_hour_ago.Month(), one_hour_ago.Day(),
		one_hour_ago.Hour(), one_hour_ago.Minute(), one_hour_ago.Second())
	url = e.mobytEndpoint + history_uri + "?from=" + date
	sms_sent := getSMSLastHourSent(mobytRequest(url, auth))
	ch <- prometheus.MustNewConstMetric(
		smsSent, prometheus.GaugeValue, float64(sms_sent), "",
	)

	log.Println("Endpoint scraped")
}

func main() {
	flag.Parse()

	configFile := *configPath
	if configFile != "" {
		log.Printf("Loading %s env file.\n", configFile)
		if err := godotenv.Load(configFile); err != nil {
			log.Printf("Error loading %s env file.\n", configFile)
		}
	} else {
		if err := godotenv.Load(); err != nil {
			log.Printf("Error loading .env file, assume env variable are set.")
		}
	}

	mobytEndpoint := os.Getenv("MOBYT_ENDPOINT")
	mobytUsername := os.Getenv("MOBYT_USERNAME")
	mobytPassword := os.Getenv("MOBYT_PASSWORD")

	exporter := NewExporter(mobytEndpoint, mobytUsername, mobytPassword)
	prometheus.MustRegister(exporter)
	log.Printf("Using connection endpoint: %s", mobytEndpoint)

	http.Handle(*metricsPath, promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
		<head><title>Mobyt Exporter</title></head>
		<body>
		<h1>Mobyt Exporter,/h1>
		<p><a href='` + *metricsPath + `'>Metrics</a></p>
		</body>
		</html>`))
	})
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
