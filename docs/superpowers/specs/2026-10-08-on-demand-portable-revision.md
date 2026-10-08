# Portable on-demand activation — approved revision

This revision supersedes the packet-observation/backend portions of
`2026-10-08-on-demand-containers-design.md`. The user explicitly chose broad Linux
compatibility and native running traffic after reviewing the feasibility results.
The group lifecycle, exact-container operations, self-protection, durable holds,
coordination, recovery, authentication and bounded-resource requirements remain.

## Accepted behavior

- While a group sleeps, Porty binds its fixed published TCP/UDP ports with ordinary
  Go network listeners. No packet capture, eBPF, compiler, kernel-version test,
  proxy, firewall rule, route, interface or sysctl changes are required.
- One UDP datagram or one **completed TCP connection** wakes by default. An isolated
  SYN is no longer a supported trigger. The configurable threshold counts these
  wake attempts in the first-attempt window; it does not count TCP wire packets.
- The wake remains latched if an event notification is lost or admission is busy.
  Revision/generation checks invalidate stale work. Close all group listeners
  under coordinated admission before Docker starts the exact existing containers.
- Close accepted TCP connections promptly; bound connection memory and work. Client
  retries remain necessary: Porty does not impersonate Minecraft or buffer/replay
  requests. Boot/readiness time is separate from wake-detection time.
- Running traffic follows the existing Docker path. Container-wide network byte
  counters from the official Docker SDK determine activity. Any changed member
  counter refreshes the group's idle deadline. Background traffic can keep a
  group awake. Missing, stale or reset counters cannot authorize a stop.
- Sample running groups at a bounded shared cadence; no per-packet work, per-player
  tracking, or game queries. Sleeping groups need no Docker stats stream. Measure
  sampling overhead before claiming the resource budget.
- After a coordinated stop, bind listeners before reporting the group sleeping.
  A bind conflict pauses automation and is visible; never steal an existing port.
  Manual stops establish durable hold; manual starts first release held ports.
  External starts may encounter a reserved port and require hold/disable first.
- Stopping Porty closes its listeners. No host network changes need cleanup. The
  feature requires the same local Docker context and host network visibility as
  before. Containers running Porty use host networking. Privileged ports retain
  the OS's ordinary binding-permission requirements.
- UDP activation initially requires equal published/container ports. The available
  Docker environment failed same-client-socket retries with remapped UDP ports,
  while equal ports passed. Reject unsupported mappings; never repair conntrack
  or silently proxy traffic. Reassess this restriction with upstream fixes and
  verified daemon versions.
- GeoIP is a future separate feature. Sleeping listeners expose source addresses
  for wake admission. Enforcing GeoIP on running native traffic will require its
  own enforcement mechanism; the portable listener cannot enforce that policy.

## Feasibility evidence (2026-10-08)

Environment: amd64, 16 CPUs, Linux 6.18.33.2 WSL2, Docker Desktop 29.5.2.
These results do not establish support for every distribution, kernel or Docker
network path.

The superseded socket-attached eBPF prototype passed single TCP SYN/UDP wake,
IPv4/IPv6, malformed/VLAN/fragment parsing, concurrent counters, actual ring-buffer
overflow with a lone final packet, generation invalidation, non-root file-capability
execution, repeated descriptor cleanup, and normal-exit/SIGKILL resource reclamation.
Runtime capabilities were BPF and NET_RAW. A separate cleanup observer needed
SYS_ADMIN for global ID lookups; the monitored child dropped it before loading.

Five alternating rounds of 64-byte UDP loopback ping/pong gave median packet rates
of 92.4% of native for an empty socket filter, 86.3% for matched monitored traffic,
and 87.9% for unrelated traffic. Added p99 RTT was about 5 microseconds. Idle CPU
was 0.0096% of one core in that probe. These are local transaction-rate measurements,
not physical-interface bandwidth results. The warmed RSS delta was negative and
is not evidence of a peak-memory bound. The 98% performance gate was not met.

Docker's private bridge-address publication failed in Desktop's forwarding layer.
A separate loopback test established working UDP before sleep, exact-ID restart
after a single datagram, and successful same-source-socket retries with equal
host/container ports. Remapped UDP ports failed after restart. This is consistent
with the reported [Moby conntrack port regression](https://github.com/moby/moby/issues/53384),
not proof that every affected daemon/network combination has the same root cause.
No conntrack, firewall, route or sysctl repair was attempted.

## Revised delivery sequence

1. Implement and test portable sleeping listeners, reservation cleanup, threshold
   semantics, same-socket UDP retry, and native running-path/resource measurements.
2. Implement policy validation and conservative group lifecycle decisions.
3. Persist policies/holds/intents and exact runtime provenance; add Docker stats
   and exact start/stop/readiness methods through the existing SDK adapter.
4. Reuse operation/coordinator admission, manual/deploy/update interlocks, recovery
   and alerts; then wire app lifetime and authenticated API.
5. Add stack settings and status, operator documentation, and full verification.

Do not claim release readiness until the revised end-to-end gates pass. The former
implementation plan remains a checklist for tasks2–8, with its eBPF monitor
contract replaced by sleeping listeners and runtime network-counter sampling.
