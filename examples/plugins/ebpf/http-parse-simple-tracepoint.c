/* generated from sudo /usr/share/bcc/tools/argdist -v -C 't:skb:skb_copy_datagram_iovec():int:tp.len' -i 2 */
struct __string_t { char s[80]; };

#include <uapi/linux/ptrace.h>
#include <linux/skbuff.h>


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
  u32 pid;
};
BPF_HASH(perf_trace_skb_copy_datagram_iovec_hash0, struct perf_trace_skb_copy_datagram_iovec_hash0_key_t, u64);


/* TODO: Can't we optimize this and do all the work directly in __trace_entry_update? */
int perf_trace_skb_copy_datagram_iovec_probe0(struct pt_regs *ctx )
{


  u64 tid = bpf_get_current_pid_tgid();
  u64 *di = __trace_di.lookup(&tid);
  if (di == 0) { return 0; }
  struct skb_copy_datagram_iovec_trace_entry tp = {};
  bpf_probe_read(&tp, sizeof(tp), (void *)*di);
  /* TODO: exit early if it's not TCP */
  const struct sk_buff *skb = tp.skbaddr;
  /* TODO: offset is missing from tp,
     this may result in incorrect request metrics

     http://stackoverflow.com/questions/25047905/http-request-minimum-size-in-bytes
     minimum length of http request is always geater than 7 bytes
     avoid invalid access memory
     include empty payload
  */

  /* Explicit implementation of skb_headlen() */
  unsigned int skb_len = 0;
  unsigned int skb_data_len = 0;
  bpf_probe_read(&skb_len, sizeof(skb_len), &skb->len);
  bpf_probe_read(&skb_data_len, sizeof(skb_data_len), &skb->data_len);
  unsigned int head_len = skb_len - skb_data_len;
  /* Print debug info (made available at /sys/kernel/debug/tracing/trace) */
  bpf_trace_printk("Head_len  %u\n", head_len);
  if (head_len < 7) {
    return 0;
  }

  u8 data[4] = {0, 0, 0, 0};
  bpf_probe_read(&data, sizeof(data), skb->data);
  /* find a match with an HTTP message */
  bpf_trace_printk("Data1 %x %x\n", data[0], data[1]);
  bpf_trace_printk("Data2 %x %x\n", data[2], data[3]);

  if ((data[0] != 'G') || (data[1] != 'E') || (data[2] != 'T')) {
    return 0;
  }

  /* Record request */
  struct perf_trace_skb_copy_datagram_iovec_hash0_key_t __key = {};
  __key.pid = tid & 0xFFFF;

  perf_trace_skb_copy_datagram_iovec_hash0.increment(__key);
  return 0;
}

/*
open uprobes: {}
open kprobes: {'p_tracing_generic_entry_update': c_void_p(33919360), 'r_perf_trace_skb_copy_datagram_iovec': c_void_p(33946304)}
*/
