/* generated from sudo /usr/share/bcc/tools/argdist -v -C 't:skb:skb_copy_datagram_iovec():int:tp.len' -i 2 */
struct __string_t { char s[80]; };

#include <uapi/linux/ptrace.h>

BPF_HASH(__trace_di, u64, u64);

int __trace_entry_update(struct pt_regs *ctx)
{
  u64 tid = bpf_get_current_pid_tgid();
  u64 val = ctx->di;
  __trace_di.update(&tid, &val);
  return 0;
}

struct skb_copy_datagram_iovec_trace_entry {
  u64 __do_not_use__;
  const void * skbaddr;
  int len;

};

struct perf_trace_skb_copy_datagram_iovec_hash0_key_t {
  int v0;
};
BPF_HASH(perf_trace_skb_copy_datagram_iovec_hash0, struct perf_trace_skb_copy_datagram_iovec_hash0_key_t, u64);


int perf_trace_skb_copy_datagram_iovec_probe0(struct pt_regs *ctx )
{


  u64 tid = bpf_get_current_pid_tgid();
  u64 *di = __trace_di.lookup(&tid);
  if (di == 0) { return 0; }
  struct skb_copy_datagram_iovec_trace_entry tp = {};
  bpf_probe_read(&tp, sizeof(tp), (void *)*di);
  const void * skbaddr = tp.skbaddr;
  int len = tp.len;


  if (!(1)) return 0;
  struct perf_trace_skb_copy_datagram_iovec_hash0_key_t __key = {};
  __key.v0 = tp.len;

  perf_trace_skb_copy_datagram_iovec_hash0.increment(__key);
  return 0;
}

/*
open uprobes: {}
open kprobes: {'p_tracing_generic_entry_update': c_void_p(33919360), 'r_perf_trace_skb_copy_datagram_iovec': c_void_p(33946304)}
*/
