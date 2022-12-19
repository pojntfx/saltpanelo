# Terminology

- **Router**: Control plane; keeps track of topology and data flow/metrics, answers requests for optimal connection paths
- **Switch**: Data plane; forwards packets along a specified route, keeps connections alive
- **Gateway**: Data & control ingress and egress; clients connect to this, asks router for optimal connection path and forwards packets to switches (includes an embedded switch to skip the hop if possible)
- **Adapter**: Client; listens/dials a service on the client and forwards packets to/from the gateway
