# CloudOpsCrew frontend simulation

A React + TypeScript + Vite static simulation of the CloudOpsCrew two-node Cloud Hypervisor VM controller. This is a frontend-only demonstration: charts, metrics, alerts and VM records are illustrative and no controller API or AWS actions are invoked.

## Run locally

```bash
cd frontend
npm install
npm run dev
```

## Build

```bash
npm run build
npm run preview
```

The Vite base path is configured for the repository GitHub Pages project site at `/A3-PS023-TC061/`.

## Pages

- Landing page and product overview
- Documentation overview and architecture
- Demo sign-in screen (no authentication)
- Dashboard and worker health
- Instance inventory and simulated actions
- Monitoring charts for CPU, memory, network and storage
- Alerts and acknowledgement interactions
- Security and cluster settings

All data and controls are demo-only. Production operations continue to belong to the Go controller.
