# Mobyt Exporter

This is a prometheus like exporter. It aims to get information from mobyt API 
and expose its data into a web page that will be scrapped by Prometheus.

## Usage 

Deploy the exporter wihtin a container 

```bash
docker-compose up -d 
curl -IL localhost:9141/metrics
```

## Indicators 

| indicator   | Description                       |
|-------------|-----------------------------------|
| sms_credit  | Number of remaining sms           |
| sms_money   | Money in the account              |
| sms_history | Number of sms sent since 1h/1d/1M |


