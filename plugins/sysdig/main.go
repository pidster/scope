package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

func main() {
	hostname, _ := os.Hostname()
	var (
		addr   = flag.String("addr", "/var/run/scope/plugins/sysdig.sock", "unix socket to listen for connections on")
		hostID = flag.String("hostname", hostname, "hostname of the host running this plugin")
	)
	flag.Parse()

	log.Println("Starting...")

	// If sysdig exists
	which := exec.Command("which", "sysdig")
	which.Stderr = os.Stderr
	if err := which.Run(); err != nil {
		log.Fatalf("sysdig: error starting: %v", err)
	}

	log.Println("Found sysdig...")

	os.Remove(*addr)
	l, err := net.Listen("unix", *addr)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		l.Close()
		os.Remove(*addr)
	}()

	log.Printf("Listening on: unix://%s", *addr)

	httpLog, err := newHttpLog()
	if err != nil {
		log.Println(err)
		return
	}
	rpc.Register(&Plugin{httpLog: httpLog, HostID: *hostID})
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Printf("error: %v", err)
			break
		}
		go rpc.ServeCodec(jsonrpc.NewServerCodec(conn))
	}
}

type Plugin struct {
	httpLog *httpLog
	HostID  string
}

func (p *Plugin) Handshake(args map[string]string, resp *map[string]interface{}) error {
	log.Printf("Probe %s handshake", args["probe_id"])
	(*resp) = map[string]interface{}{
		"name":        "sysdig",
		"description": "Displays HTTP request rates by process based on sysdig data",
		"interfaces":  []string{"reporter"},
		"api_version": "1",
	}
	return nil
}

func (p *Plugin) Report(args map[string]string, resp *map[string]interface{}) error {
	now := time.Now()
	nowISO := now.Format(time.RFC3339)
	nodes := map[string]interface{}{}
	// TODO: divide this by path? or aggregate by endpoint
	p.httpLog.ForEach(now, func(pid string, txns []*Transaction) {
		inbound, outbound := 0, 0
		for _, txn := range txns {
			if txn.Direction == "<" {
				inbound++
			} else {
				outbound++
			}
		}
		nodes[p.HostID+";"+pid] = map[string]interface{}{
			"latest": map[string]interface{}{
				"http_rate_inbound": latest{
					Timestamp: nowISO,
					Value:     fmt.Sprint(inbound),
				},
				"http_rate_outbound": latest{
					Timestamp: nowISO,
					Value:     fmt.Sprint(outbound),
				},
			},
		}
	})
	(*resp) = map[string]interface{}{
		"Process": map[string]interface{}{
			"nodes": nodes,
			"metadata_templates": map[string]interface{}{
				"http_rate_inbound": map[string]interface{}{
					"id":       "http_rate_inbound",
					"label":    "In HTTP Rate",
					"from":     "latest",
					"priority": 10,
				},
				"http_rate_outbound": map[string]interface{}{
					"id":       "http_rate_outbound",
					"label":    "Out HTTP Rate",
					"from":     "latest",
					"priority": 11,
				},
			},
		},
	}
	return nil
}

type latest struct {
	Timestamp string `json:"timestamp"`
	Value     string `json:"value"`
}

type httpLog struct {
	sync.Mutex
	wg           sync.WaitGroup
	command      *exec.Cmd
	transactions map[string][]*Transaction
}

func newHttpLog() (*httpLog, error) {
	h := &httpLog{
		command:      exec.Command("sysdig", "-c", "http_txns_by_pid"),
		transactions: make(map[string][]*Transaction),
	}
	stdout, err := h.command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := h.command.StderrPipe()
	if err != nil {
		return nil, err
	}
	go h.readLoop(stdout, h.onEvent)
	go h.readLoop(stderr, func(line string) {
		log.Println("sysdig: %s", line)
	})
	h.wg.Add(2)
	return h, h.command.Start()
}

// ForEach calls f on each pid's set of transactions, removing outdated
// transactions as it goes.
func (h *httpLog) ForEach(now time.Time, f func(string, []*Transaction)) {
	h.Lock()
	expiry := now.Add(-1 * time.Second)
	for pid, txns := range h.transactions {
		result := make([]*Transaction, 0, len(txns))
		for _, txn := range txns {
			if txn.Timestamp.After(expiry) {
				result = append(result, txn)
			}
		}
		if len(result) == 0 {
			delete(h.transactions, pid)
			continue
		}
		h.transactions[pid] = result
		f(pid, result)
	}
	h.Unlock()
}

func (h *httpLog) readLoop(r io.Reader, f func(string)) {
	lines := bufio.NewScanner(r)
	for lines.Scan() {
		f(lines.Text())
		if err := lines.Err(); err != nil {
			log.Printf("httplog: error: %s", err)
		}
	}
}

func (h *httpLog) onEvent(line string) {
	if line == "" {
		return
	}
	if txn, err := parseTransaction(line); err != nil {
		log.Printf("httplog: error: %s on input %q", err, line)
	} else {
		h.Lock()
		h.transactions[txn.PID] = append(h.transactions[txn.PID], txn)
		h.Unlock()
	}
}

func (h *httpLog) Close() error {
	if h.command.Process != nil {
		return h.command.Process.Kill()
	}
	h.wg.Wait()
	return h.command.Wait()
}

type Transaction struct {
	Timestamp    time.Time
	Container    string
	Direction    string
	PID          string
	Method       string
	URL          string
	Host         string
	ResponseCode int
	Latency      time.Duration
	Size         int
}

func parseTransaction(raw string) (*Transaction, error) {
	fields := strings.Fields(raw)
	if len(fields) <= 1 {
		return nil, fmt.Errorf("incorrect number of fields in transaction entry: %d", len(fields))
	}

	// rawtime=1457356540698730752 container=abcd direction=> pid=25247 method=GET url=/ host=searchapp:8080 response_code=200 latency=1ms size=25
	txn := &Transaction{}
	parsers := map[string]func(v string) error{
		"rawtime": func(v string) error {
			nano, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return err
			}
			txn.Timestamp = time.Unix(nano/int64(time.Second), nano%int64(time.Second))
			return nil
		},
		"container": func(v string) error { txn.Container = v; return nil },
		"direction": func(v string) error { txn.Direction = v; return nil },
		"pid":       func(v string) error { txn.PID = v; return nil },
		"method":    func(v string) error { txn.Method = v; return nil },
		"url":       func(v string) error { txn.URL = v; return nil },
		"host":      func(v string) error { txn.Host = v; return nil },
		"response_code": func(v string) (err error) {
			txn.ResponseCode, err = strconv.Atoi(v)
			return err
		},
		"latency": func(v string) (err error) {
			txn.Latency, err = time.ParseDuration(v)
			return err
		},
		"size": func(v string) (err error) {
			txn.Size, err = strconv.Atoi(v)
			return err
		},
	}
	for _, field := range fields {
		i := strings.IndexByte(field, '=')
		if i == -1 {
			continue
		}
		key, value := field[:i], field[i+1:]
		parser, ok := parsers[key]
		if !ok {
			return nil, fmt.Errorf("Unknown transaction field: %s\n", field)
		}
		if err := parser(value); err != nil {
			return nil, err
		}
	}
	return txn, nil
}
