package main

import (
	"bytes"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/miekg/dns"
	"github.com/spf13/cobra"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/logging"
	"go.uber.org/atomic"
	"go.uber.org/zap"
	"golang.org/x/exp/slices"
)

// Injected at build time
var version = ""

var zlog, tracer = logging.RootLogger("dnsf", "github.com/maoueh/dnsf")

func main() {
	logging.InstantiateLoggers()

	Run(
		"dnsf",
		"Runs a DNS server that answer queries from the loaded zone file.",

		ConfigureVersion(version),
		ConfigureViper("DNSF"),

		Command(runE,
			"run <records_file> [<port>]",
			"Loads the DNS records file and starts the DNS server",
			Description(`
				The records file is line based and the spec followed is RFC 1035,
				please refer to it for the format of each record.

				Example of a zone file:

					$ORIGIN matt.local.     ; designates the start of this zone file in the namespace
					$TTL 3600                ; default expiration time (in seconds) of all RRs without their own TTL value
					matt.local.  IN  SOA   ns.matt.local. username.matt.local. ( 2020091025 7200 3600 1209600 3600 )
					matt.local.  IN  NS    ns
					matt.local.  IN  A     127.0.0.1
					ns            IN  A     127.0.0.1
					workers            IN  A     12.0.0.2
					workers            IN  A     12.0.0.3

				The managed zones are infered found the records file directly and each unique zone is
				handled by the server.

				You can test the server with the following command:

					dig -p 8053 @127.0.0.1 matt.local A
					dig -p 8053 @127.0.0.1 workers.matt.local A
			`),
			ExamplePrefixed("dnsf", `
				fake records_file.zone
			`),
			RangeArgs(1, 2),
		),

		OnCommandErrorLogAndExit(zlog),
	)
}

func runE(cmd *cobra.Command, args []string) error {
	recordsFile := args[0]
	port := 8053
	if len(args) == 2 {
		var err error
		port, err = strconv.Atoi(args[1])
		NoError(err, "Invalid port number")
	}

	zonesData, err := os.ReadFile(recordsFile)
	NoError(err, "Failed to read zone file")

	zp := dns.NewZoneParser(bytes.NewReader(zonesData), "", "")

	var records []dns.RR
	for rr, ok := zp.Next(); ok; rr, ok = zp.Next() {
		records = append(records, rr)
	}
	NoError(zp.Err(), "Parsing zone file failed")

	soaIdx := slices.IndexFunc(records, func(r dns.RR) bool {
		return r.Header().Rrtype == dns.TypeSOA
	})
	Ensure(soaIdx != -1, "No SOA record found in zone file")

	soa := records[soaIdx].(*dns.SOA)

	bySubZones := map[string][]dns.RR{}
	for _, rr := range records {
		zlog.Debug("Loaded DNS record", zap.Stringer("rr", rr))
		zoneID := rr.Header().Name
		bySubZones[zoneID] = append(bySubZones[zoneID], rr)
	}

	for zoneID, records := range bySubZones {
		zlog.Debug("Zone", zap.String("zone_id", zoneID), zap.Any("records", records))

		dnsZoneHandler(zoneID, records, soa)
	}

	go func() {
		zlog.Info("Starting DNS server (UDP)", zap.Int("port", port))
		srv := &dns.Server{Addr: ":" + strconv.Itoa(port), Net: "udp"}
		err := srv.ListenAndServe()
		cli.NoError(err, "Failed to set UDP listener")
	}()

	go func() {
		zlog.Info("Starting DNS server (TCP)", zap.Int("port", port))
		srv := &dns.Server{Addr: ":" + strconv.Itoa(port), Net: "tcp"}
		err := srv.ListenAndServe()
		cli.NoError(err, "Failed to set TCP listener")
	}()

	zlog.Info("Waiting for termination signal")
	sig := make(chan os.Signal, 10)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	zlog.Info("Terminating")

	return nil
}

var globalID = atomic.NewUint64(0)

func dnsZoneHandler(zoneID string, records []dns.RR, soa *dns.SOA) {
	logger := zlog.With(zap.String("zone_id", zoneID))

	recordsByType := map[uint16][]dns.RR{}
	for _, rr := range records {
		recordsByType[rr.Header().Rrtype] = append(recordsByType[rr.Header().Rrtype], rr)
	}

	dns.HandleFunc(zoneID, func(w dns.ResponseWriter, r *dns.Msg) {
		requestID := globalID.Inc()
		logger := logger.With(zap.Uint64("request_id", requestID))

		hasQuestion := len(r.Question) > 0
		questionType := uint16(0)
		if hasQuestion {
			questionType = r.Question[0].Qtype
		}

		logger.Info("Received DNS query", zap.Bool("has_question", hasQuestion), zap.String("question_type", dns.TypeToString[questionType]))

		m := new(dns.Msg)
		m.SetReply(r)
		m.Authoritative = true
		m.Ns = []dns.RR{soa}
		m.Answer = recordsByType[r.Question[0].Qtype]

		logger.Info("Answering DNS query", zap.Any("msg", m.Answer))

		w.WriteMsg(m)
	})
}
