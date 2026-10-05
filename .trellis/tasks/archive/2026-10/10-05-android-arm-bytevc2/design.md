# Design

Use one disposable redroid amd64 container with an ARM native bridge, software graphics, bounded CPU/RAM and localhost-only ADB. Keep all data/tool downloads under a dedicated temporary directory. First prove boot and native ARM execution, then load official ARM SDK dependencies and query decoder availability. SDK metadata/frame callbacks do not count as decoded YUV frames. Avoid host kernel settings or production Docker container changes. Remove the test container, temporary media and downloaded tools after recording results; remove newly pulled images when unused.
