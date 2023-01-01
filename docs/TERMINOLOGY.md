# Terminology

- **Control plane**:
  - **Router**: Keeps track of switch topology and measures network latency
  - **Gateway**: Keeps track of adapter topology and does routing
  - **Metrics**: Exposes the topology graph
  - **Visualizer**: Renders the topology graph
- **Data plane**:
  - **Switch**: Forwards packets from adapters to switches and switches to switches
  - **Adapter**: Serves as ingress/egress
