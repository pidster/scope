package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"
)

func main() {
	hostname, _ := os.Hostname()
	var (
		addr   = flag.String("addr", "/var/run/scope/plugins/ebpf.sock", "unix socket to listen for connections on")
		hostID = flag.String("hostname", hostname, "hostname of the host running this plugin")
	)
	flag.Parse()

	log.Println("Starting...")

	// Compile and run the ebpf script
	ebpf := exec.Command("http-parse-simple.py")
	ebpf.Stderr = os.Stderr
	stdout, err := ebpf.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	if err := ebpf.Start(); err != nil {
		log.Fatal(err)
	}
	defer func() {
		if ebpf.Process != nil {
			ebpf.Process.Kill()
		}
	}()

	os.Remove(*addr)
	listener, err := net.Listen("unix", *addr)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		listener.Close()
		os.Remove(*addr)
	}()

	log.Printf("Listening on: unix://%s", *addr)

	plugin := &Plugin{HostID: *hostID}
	go plugin.loop(stdout)
	http.HandleFunc("/", plugin.Handshake)
	http.HandleFunc("/report", plugin.Report)
	if err := http.Serve(listener, nil); err != nil {
		log.Printf("error: %v", err)
	}
	ebpf.Wait()
}

type Plugin struct {
	sync.Mutex
	HostID string
	tuples []tuple
}

func (p *Plugin) Handshake(w http.ResponseWriter, r *http.Request) {
	log.Printf("Probe %s handshake", r.FormValue("probe_id"))
	err := json.NewEncoder(w).Encode(map[string]interface{}{
		"name":        "ebpf",
		"description": "Adds a graph of http requests/second to processes",
		"interfaces":  []string{"reporter"},
		"api_version": "1",
	})
	if err != nil {
		log.Printf("error: %v", err)
	}
}

func (p *Plugin) Report(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	nowISO := now.Format(time.RFC3339)
	tuples := p.current()
	counts := map[string]float64{}
	for _, t := range tuples {
		counts[fmt.Sprintf(";%s;%d", t.serverIP, t.serverPort)]++
	}
	nodes := map[string]interface{}{}
	for id, c := range counts {
		nodes[id] = map[string]interface{}{
			"metrics": map[string]interface{}{
				"http_requests_per_second": map[string]interface{}{
					"samples": []interface{}{
						map[string]interface{}{
							"date":  nowISO,
							"value": c,
						},
					},
				},
			},
		}
	}
	err := json.NewEncoder(w).Encode(map[string]interface{}{
		"Endpoint": map[string]interface{}{
			"nodes": nodes,
		},
		"Process": map[string]interface{}{
			"metric_templates": map[string]interface{}{
				"http_requests_per_second": map[string]interface{}{
					"id":       "http_requests_per_second",
					"label":    "HTTP Req/Second",
					"priority": 0.1, // low number so it shows up first
				},
			},
		},
	})
	if err != nil {
		log.Printf("error: %v", err)
	}
}

// scan tuples from the ebpf plugin
func (p *Plugin) loop(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fmt.Printf("Got line: %s\n", scanner.Text())
		var serverIP, clientIP string
		var serverPort, clientPort int
		_, err := fmt.Sscanf(scanner.Text(), "%s %d %s %d", &serverIP, &serverPort, &clientIP, &clientPort)
		if err != nil {
			log.Fatal(err)
		}
		p.Lock()
		t := tuple{
			timestamp:  time.Now(),
			serverIP:   serverIP,
			serverPort: serverPort,
			clientIP:   clientIP,
			clientPort: clientPort,
		}
		fmt.Printf("Got Tuple: %+v\n", t)
		p.tuples = append(p.tuples, t)
		p.Unlock()
	}
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
}

func (p *Plugin) current() []tuple {
	p.Lock()
	now := time.Now()
	expiry := now.Add(-1 * time.Second)
	// Garbage collect old tuples
	for i := 0; i < len(p.tuples); i++ {
		if p.tuples[0].timestamp.After(expiry) {
			break
		}
		p.tuples = p.tuples[1:]
	}

	result := make([]tuple, len(p.tuples))
	copy(result, p.tuples)
	p.Unlock()
	return result
}

type tuple struct {
	timestamp  time.Time
	serverIP   string
	serverPort int
	clientIP   string
	clientPort int
}
