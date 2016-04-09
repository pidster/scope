#!/usr/bin/env python
import bcc

import time
import datetime
import os
import json
import urlparse
import threading
import socket
import BaseHTTPServer
import SocketServer
import sys

EBPF_FILE = "http-requests.c"
EBPF_TABLE_NAME = "received_http_requests"
PLUGIN_UNIX_SOCK = "/var/run/scope/plugins/http_requests.sock"

class KernelInspector(threading.Thread):
    def __init__(self):
        super(KernelInspector, self).__init__()
        self.bpf = bcc.BPF(EBPF_FILE)
        self.http_rate_per_pid = dict()
        self.lock = threading.Lock()

    def update_http_rate_per_pid(self, last_req_count_snapshot):
        new_req_count_snapshot = dict()
        new_http_rate_per_pid = dict()
        req_count_table = self.bpf.get_table(EBPF_TABLE_NAME)
        for key, value in req_count_table.iteritems():
            if key.pid in last_req_count_snapshot:
                new_http_rate_per_pid[key.pid] = value.value - last_req_count_snapshot[key.pid]
            else:
                new_http_rate_per_pid[key.pid] = value.value

            new_req_count_snapshot[key.pid] = value.value

        self.lock.acquire()
        self.http_rate_per_pid = new_http_rate_per_pid
        self.lock.release()

        return new_req_count_snapshot

    def on_http_rate_per_pid(self, f):
        self.lock.acquire()
        r = f(self.http_rate_per_pid)
        self.lock.release()
        return r

    def run(self):
        # Compute request rates based on the requests counts from the last
        # second. It would be simpler to clear the table, wait one second
        # clear() is expensive (each entry is individually cleared with a system
        # call) and less robust (clearing contends with the increments done by
        # the kernel probe).
        # FIXME: we need a mechanism to garbage-collect old processes, either
        #        here or on the probe. Some options are clearing the table once
        #        in a while (not ideal for the reasons above) or adding another
        #        probe to remove processes from the table when they die (this
        #        will probably require keeping keeping track of tasks and not
        #        just processes)
        req_count_snapshot = dict()
        while True:
            time.sleep(1)
            req_count_snapshot = self.update_http_rate_per_pid(req_count_snapshot)


class PluginRequestHandler(BaseHTTPServer.BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.1'

    def do_GET(self):
        path = urlparse.urlparse(self.path)[2].lower()
        if path == '/':
            self.do_handshake()
        elif path == '/report':
            self.do_report()
        else:
            self.send_response(404)
            self.send_header('Content-length', 0)
            self.end_headers()

    def get_process_nodes(self, http_rate_per_pid):
        # Get current timestamp in RFC3339
        date = datetime.datetime.utcnow()
        date = date.isoformat('T') + 'Z'
        process_nodes = dict()
        for pid, http_rate in http_rate_per_pid.iteritems():
            node_key = "%d;<%s>" % (pid, self.server.hostname)
            process_nodes[node_key] = {
                'metrics': {
                    'http_requests_per_second': {
                        'samples': [{
                            'date': date,
                            'value': http_rate,
                        }]
                    }
                }
            }
        return process_nodes

    def do_report(self):
        kernel_inspector = self.server.kernel_inspector
        process_nodes = kernel_inspector.on_http_rate_per_pid(self.get_process_nodes)
        report = {
            'Process': {
                'nodes': process_nodes,
                'metric_templates': {
                    'http_requests_per_second': {
                        'id':       'http_requests_per_second',
                        'label':    'HTTP Req/Second',
                        'priority': 0.1,
                    }
                }
            }
        }
        self.respond(json.dumps(report))

    def do_handshake(self):
        spec = {
            'name': 'http-requests',
            'description': 'Adds http request metrics to processes',
            'interfaces': ['reporter'],
            'api_version': '1',
        }
        self.respond(json.dumps(spec))

    def respond(self, body):
        self.send_response(200)
        self.send_header('Content-type', 'application/json')
        self.send_header('Content-length', len(body))
        self.end_headers()
        self.wfile.write(body)


class PluginServer(SocketServer.UnixStreamServer):
    def __init__(self, socket_file, kernel_inspector):
        if os.path.exists(socket_file):
            os.remove(socket_file)
        SocketServer.UnixStreamServer.__init__(self, socket_file, PluginRequestHandler)
        self.kernel_inspector = kernel_inspector
        self.hostname = socket.gethostname()

    def finish_request(self, request, _):
        # Make the logger happy by providing a phony client_address
        self.RequestHandlerClass(request, '-', self)


if __name__ == '__main__':
    try:
        kernel_inspector = KernelInspector()
        kernel_inspector.setDaemon(True)
        kernel_inspector.start()
        PluginServer(PLUGIN_UNIX_SOCK, kernel_inspector).serve_forever()
    except KeyboardInterrupt:
        pass
