package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	zabbix "github.com/AlekSi/zabbix-sender"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	exporterType      = flag.String("type", "prometheus", "Exporter type : prometheus or zabbix")
	listenAddress     = flag.String("web.listen", ":4242", "Address on which to expose metrics and web interface.")
	metricsPath       = flag.String("web.path", "/metrics", "Path under which to expose metrics.")
	enabledCollectors = flag.String("collect", "dummy,chassis,memory,processors,ps,ps_amps_sysboard_pwr,storage_battery,storage_enclosure,storage_vdisk,system,temps,volts", "Comma-separated list of collectors to use.")
	zabbixFromHost    = flag.String("zabbix.from", getFQDN(), "Send to Zabbix from this host name. You can also set HOSTNAME and DOMAINNAME environment variables.")
	zabbixServer      = flag.String("zabbix.server", "localhost", "Zabbix server hostname or address")
	cache             = NewMetricStorage()

	collectors = map[string]Collector{
		"dummy":      Collector{F: dummy_report},
		"chassis":    Collector{F: c_omreport_chassis},
		"fans":       Collector{F: c_omreport_fans},
		"memory":     Collector{F: c_omreport_memory},
		"processors": Collector{F: c_omreport_processors},
		"ps":         Collector{F: c_omreport_ps},
		"ps_amps_sysboard_pwr": Collector{F: c_omreport_ps_amps_sysboard_pwr},
		"storage_battery":      Collector{F: c_omreport_storage_battery},
		"storage_controller":   Collector{F: c_omreport_storage_controller},
		"storage_enclosure":    Collector{F: c_omreport_storage_enclosure},
		"storage_vdisk":        Collector{F: c_omreport_storage_vdisk},
		"system":               Collector{F: c_omreport_system},
		"temps":                Collector{F: c_omreport_temps},
		"volts":                Collector{F: c_omreport_volts},
	}
)

type metricStorage struct {
	Lock    sync.RWMutex
	metrics map[string]interface{}
}

func NewMetricStorage() *metricStorage {
	ms := new(metricStorage)
	ms.metrics = make(map[string]interface{})
	return ms
}

type Collector struct {
	F func() error
}

func Add(name string, value string, t prometheus.Labels, desc string) {

	switch *exporterType {

	case "prometheus":
		log.Println("Adding metric : ", name, t, value)
		d := prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace:   "dell",
			Subsystem:   "hw",
			Name:        name,
			Help:        desc,
			ConstLabels: t,
		})
		floatValue, err := strconv.ParseFloat(value, 64)
		if err != nil {
			log.Println("Could not parse value for metric ", name)
			return
		}
		d.Set(floatValue)
		prometheus.MustRegister(d)

	case "zabbix":
		cache.Lock.Lock()
		defer cache.Lock.Unlock()
		zabbixMetricName := "hw." + strings.Replace(name, "_", ".", -1)
		for _, v := range t {
			zabbixMetricName += "." + v
		}
		cache.metrics[zabbixMetricName] = value
	}

}

func collect(collectors map[string]Collector) error {
	for _, name := range strings.Split(*enabledCollectors, ",") {
		collector := collectors[name]
		log.Println("Running collector", name)
		err := collector.F()
		if err != nil {
			return err
		}
	}
	return nil
}

func main() {
	flag.Parse()
	err := collect(collectors)
	if err != nil {
		log.Println("Step 1 - Read probe configuration failed")
		os.Exit(1)
	}

	switch *exporterType {
	case "prometheus":
		http.Handle(*metricsPath, prometheus.Handler())
		log.Print("listening to ", *listenAddress)
		log.Fatal(http.ListenAndServe(*listenAddress, nil))

	case "zabbix":
		cache.Lock.Lock()
		di := zabbix.MakeDataItems(cache.metrics, *zabbixFromHost)
		cache.Lock.Unlock()
		addr, _ := net.ResolveTCPAddr("tcp", *zabbixServer)
		res, err := zabbix.Send(addr, di)
		if err != nil {
			log.Println("Step 4 - Sent to Zabbix Server failed : ", err)
			os.Exit(4)
		}
		fmt.Print(*res)
	}
}
