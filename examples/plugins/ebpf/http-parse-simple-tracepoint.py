# stripped-down version of: sudo /usr/share/bcc/tools/argdist -v -C 't:skb:skb_copy_datagram_iovec():int:tp.len' -i 2

from bcc import BPF, Tracepoint
from time import sleep, strftime

b = BPF(src_file = "http-parse-simple-tracepoint.c")

tp_category = "skb"
tp_event = "skb_copy_datagram_iovec"
probe_table_name = "perf_trace_skb_copy_datagram_iovec_hash0"
probe_fn_name = "perf_trace_skb_copy_datagram_iovec_probe0"

Tracepoint.enable_tracepoint(tp_category, tp_event)
b.attach_kretprobe(event="perf_trace_" + tp_event, fn_name=probe_fn_name)
Tracepoint.attach(b)

while True:
    try:
        sleep(2)
    except KeyboardInterrupt:
        exit()
    print "[%s]" % strftime("%H:%M:%S")
    data = b.get_table(probe_table_name)
    data = sorted(data.items(), key=lambda kv: kv[1].value)
    for key, value in data:
        print "\t%-10s %s" % (key.pid ,str(value.value))
