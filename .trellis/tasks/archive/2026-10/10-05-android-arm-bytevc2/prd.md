# Android ARM ByteVC2 experiment

Run an isolated Android environment on this Linux x86_64 host to determine whether ARM native libraries execute through a native bridge and whether an official ByteVC2 decoder can produce usable frames.

Acceptance: document actual boot/ABI/native execution results; distinguish ARM support from ByteVC2 decode support; use only public SDK artifacts and synthetic probes; if media is tested, use only episode 81 in a temporary directory. Do not change production download queues or services. Bound resources, bind debugging to localhost, stop experimental services and remove media/SDK/debug outputs at the end. No persistent downloader integration until an actual frame/decode succeeds.
