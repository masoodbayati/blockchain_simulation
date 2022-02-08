package prometheus

import "github.com/prometheus/client_golang/prometheus"

var TotalTransaction = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "total_transaction",
		Help: "Number of get requests.",
	},
	[]string{"path"},
)

var responseStatus = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "response_status",
		Help: "Status of HTTP response",
	},
	[]string{"status"},
)

var BlockCreationDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Name: "block_creation_time",
	Help: "Duration of HTTP requests.",
}, []string{"path"})

func InitPrometheus() {
	prometheus.Register(TotalTransaction)
	prometheus.Register(responseStatus)
	prometheus.Register(BlockCreationDuration)
}
