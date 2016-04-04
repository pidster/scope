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

	plugin := &Plugin{HostID: *hostID}

	// Compile and run the ebpf script
	ifaces, err := net.Interfaces()
	if err != nil {
		log.Fatal(err)
	}
	for _, iface := range ifaces {
		log.Println("Attaching to", iface.Name)
		cmd, stdout, err := startParser(iface.Name)
		if err != nil {
			log.Fatal(err)
		}
		defer func() {
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
			cmd.Wait()
		}()
		go plugin.loop(stdout)
	}

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

	http.HandleFunc("/", plugin.Handshake)
	http.HandleFunc("/report", plugin.Report)
	if err := http.Serve(listener, nil); err != nil {
		log.Printf("error: %v", err)
	}
}

type Plugin struct {
	sync.Mutex
	HostID         string
	requestRecords []requestRecord
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
	requestRecords := p.current()
	counts := map[string]float64{}
	for _, t := range requestRecords {
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

func startParser(iface string) (*exec.Cmd, io.Reader, error) {
	cmd := exec.Command("http-parse-simple.py", iface)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	return cmd, stdout, nil
}

// scan requestRecords from the ebpf plugin
func (p *Plugin) loop(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		var serverIP, clientIP string
		var serverPort, clientPort int
		_, err := fmt.Sscanf(scanner.Text(), "%s %d %s %d", &serverIP, &serverPort, &clientIP, &clientPort)
		if err != nil {
			log.Fatal(err)
		}
		p.Lock()
		t := requestRecord{
			timestamp:  time.Now(),
			serverIP:   serverIP,
			serverPort: serverPort,
			clientIP:   clientIP,
			clientPort: clientPort,
		}
		p.requestRecords = append(p.requestRecords, t)
		p.Unlock()
	}
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
}

func (p *Plugin) current() []requestRecord {
	p.Lock()
	now := time.Now()
	expiry := now.Add(-1 * time.Second)
	// Garbage collect old requestRecords
	for i := 0; i < len(p.requestRecords); i++ {
		if p.requestRecords[0].timestamp.After(expiry) {
			break
		}
		p.requestRecords = p.requestRecords[1:]
	}

	result := make([]requestRecord, len(p.requestRecords))
	copy(result, p.requestRecords)
	p.Unlock()
	return result
}

type requestRecord struct {
	timestamp  time.Time
	serverIP   string
	serverPort int
	clientIP   string
	clientPort int
}
