# Limited Consul Server

Nomad promises independent service-discovery without consul in nomad-1.10.  That sounds awesome!

Except, it's not all it claims: this implementation of this feature is still grossly over-promised.
The discovery is useless for any actual feature, emphasizing that nomad is not a production-ready
service yet. We still need to pop up a bunch of little services, which is the microservice-storm
that Hashicorp claims to abhor.

This consul server is intentionally localhost-only, single-replica, limited to just providing the
crutch that nomad needs to act like a grown up, production-ready service, and fund all the checks
its feature-list promises have written.  Not 100% of the obligation, but enough for now.

This is as disheartening as finding that the service-discovery at Wasabi / Curio-AI was so
crippled, I had to eventually turn up a consul despite promises to the contrary -- and still had to
fight with nonfunctional mDNS, discovery across subnets, hard-coded assumptions, etc.

Pardon my clear concern with this short-coming, but it's bitten me a few times now. My recurring
optimism, informed by change notes and promises, is given the beating it deserves every time.
