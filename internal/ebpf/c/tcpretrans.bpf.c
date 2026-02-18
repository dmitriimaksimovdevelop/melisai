#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

char LICENSE[] SEC("license") = "Dual BSD/GPL";

struct event {
    u32 pid;
    u32 saddr;
    u32 daddr;
    u16 lport;
    u16 dport;
    u32 state;
    u8  type; // 1=v4, 2=v6
    char comm[16];
};

struct {
    __uint(type, BPF_MAP_TYPE_PERF_EVENT_ARRAY);
    __uint(key_size, sizeof(u32));
    __uint(value_size, sizeof(u32));
} events SEC(".maps");

// Helper to read IPv4/IPv6 from sock_common
static __always_inline int read_sock_common(struct sock_common *skc, struct event *e) {
    e->pid = bpf_get_current_pid_tgid() >> 32;
    bpf_get_current_comm(&e->comm, sizeof(e->comm));
    
    u16 family = BPF_CORE_READ(skc, skc_family);
    if (family == AF_INET) {
        e->type = 1;
        e->saddr = BPF_CORE_READ(skc, skc_rcv_saddr);
        e->daddr = BPF_CORE_READ(skc, skc_daddr);
    } else if (family == AF_INET6) {
        e->type = 2;
        // Simplified: reading only last 32 bits for demo, real implementation needs u128
        // or just ignore v6 for this simplified demo
        return 0; 
    } else {
        return 0;
    }
    
    e->lport = BPF_CORE_READ(skc, skc_num);
    e->dport = BPF_CORE_READ(skc, skc_dport);
    // dport is big-endian in kernel, convert? standard bpf_ntohs logic
    e->dport = bpf_ntohs(e->dport);
    
    e->state = BPF_CORE_READ(skc, skc_state);
    return 1;
}

SEC("kprobe/tcp_retransmit_skb")
int BPF_KPROBE(tcp_retransmit_skb, struct sock *sk, struct sk_buff *skb) {
    struct event e = {};
    struct sock_common *skc = (struct sock_common *)sk;
    
    if (read_sock_common(skc, &e)) {
        bpf_perf_event_output(ctx, &events, BPF_F_CURRENT_CPU, &e, sizeof(e));
    }
    
    return 0;
}
