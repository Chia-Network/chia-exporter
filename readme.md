# Chia Exporter

Chia Exporter is an application that is intended to run alongside a chia installation and exports prometheus style metrics based on data available from Chia RPCs. Where possible, all data is received as events from websocket subscriptions. Some data that is not available as a metrics event is also fetched as well, but usually in response to an event that was already received that indicates the data may have changed (with the goal to only make as many RPC requests as necessary to get accurate metric data).

**_This project is actively under development and relies on data that may not yet be available in a stable release of Chia Blockchain. Dev builds of chia may contain bugs or other issues that are not present in tagged releases. We do not recommend that you run pre-release/dev versions of Chia Blockchain on mission critical systems._**
