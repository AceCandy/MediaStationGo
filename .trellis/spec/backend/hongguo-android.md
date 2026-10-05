# HongGuo Android Offline Fallback

## 1. Scope / Trigger

Optional final download source after the existing three-source budget. Read together with `hongguo-catalog.md`; ordinary catalog and playback clients do not enable this controller.

## 2. Signatures

- `Client.EnableAndroidDownload(address, tools string)` configures the controller once before use.
- `Client.AndroidDownloadEnabled() bool`; `ResolveDownloadSource(..., DownloadAndroid)` resolves one numeric episode ID.
- Startup environment `MEDIASTATION_HONGGUO_ANDROID_ADB=host:port`; optional Docker target `runtime-android`, Compose override `docker-compose.hongguo-android.yml`.
- `MEDIASTATION_HONGGUO_ANDROID_TOOLS` overrides the injector directory; empty defaults to `/opt/hongguo`. For `dev.sh`, use ignored `.dev-cache` tools plus host PATH adb. `docker-compose.hongguo-android-standalone.yml` defines only Android, with a separate stable project name and persistent volume; ADB defaults to host loopback15555. For a remote emulator, bind to its trusted LAN IP and configure the same host IP/port in the backend; never expose ADB publicly. Do not start a second Go service alongside the development backend.
- Source string `android` uses existing persistence columns and UI labels; no schema migration or priority option.

## 3. Contracts

- Disabled: three sources/three tries, each source once. Enabled: same first three, Android only at try four. Transfer, verification requeue and shutdown recovery share `sourceTryLimit()`. Local IO/tool errors and cancellation never trigger an extra source. An interrupted uncheckpointed shutdown restores the consumed attempt.
- Fixed official APK 7.3.9.32/versionCode73932, native injector16.7.19 and redroid Android14 amd64 digest are pinned with hashes in deployment files. Android data is persistent; ADB is private and controlled by one project instance. Initial App agreement must be accepted by the operator, not automation.
- Start App then immediately send HOME to avoid video rendering; initialize its classloader and invoke `bk8.a.a().mGetVideoModelV2RxJava` with `seriessdk.com.dragon.read.saas.rpc.model.MGetVideoModelV2Request`. The original RPC client supplies device/signing/network behavior; do not override authorization or response methods.
- Request: `dr_scene=default`, `mixed_video_id_map={"1004":[targetNumericID]}`, `biz_param.caller_scene=download`, `video_platform=1024`, `need_all_video_definition=true`, and remaining fixed fields in embedded `download_android.js`. Request only the target episode, not an entire work.
- Response: normalize numeric/string `code=0`; select only `data[targetNumericID].video_model`. Canonical model `video_id=v0...` is a different identifier. Decode numbers with `UseNumber`, then reuse `parseDownloadAppMedia`, `DownloadRequest` and the existing verified publication pipeline. Never force all episodes to720P; highest compatible quality is per model.
- Serial waiting observes the parent task deadline/cancellation; the two-minute execution budget starts only after acquiring the gate. RPC has45-second timeout; remote process group has115-second watchdog. Cleanup precedes gate release and removes remote scripts, App process, dedicated local ADB server/socket/keys and temporary directory. Match unique command paths before killing process groups; also retain watchdog PID because the injector may already exit.
- Give ADB subprocesses a dedicated `HOME`, `TMPDIR` and Unix server socket inside the mode0700 request directory. Alpine ADB35 ignores Android-specific user-home overrides; never let keys or daemon logs fall back to shared host/container locations.
- Device connection waits for ADB readiness. Package-service diagnostic/empty output waits within the execution deadline and must not be reported as a missing/uninitialized App. Parse the exact versionCode field, accepting whitespace/end-of-line; distinguish an explicit absent package from a wrong numeric version.
- Both Compose healthchecks inspect only recent lowmemorykiller errors from the current lmkd PID. Confirmed `epoll_wait failed (errno=22)` triggers `ctl.restart lmkd`, never Android/data-volume recreation or disabling memory protection. The observed Android14 fault passed maxevents=-1 and blocked system_server writing to lmkd; recovery prevents that stalled instance from blocking subsequent requests. This is bounded service recovery, not proof that the upstream event-count defect is permanently removed.
- No URLs, keys, raw models, device IDs or raw tool errors in logs/DB/fixtures. Tool/model errors use fixed local categories. Public media address safety is unchanged; private control address is a deployment-only boundary.
- Go files must use `_android_bridge.go` / `_android_bridge_test.go`: `_android.go` and `_android_test.go` are GOOS-selected and silently excluded on Linux.

## 4. Validation & Error Matrix

- Invalid numeric IDs/control address -> reject before commands.
- Missing tools/wrong App version/uninitialized App -> safe source error, bounded final failure.
- Wrong Map target/nonzero code/unsupported model -> no accepted media.
- Cancellation during serial waiting or active request -> context error; release only owned gate slot.
- Android disconnected -> local timeout/cleanup; remote watchdog bounds abandoned work.
- Only root-origin media download failures can consume the remaining source budget; local disk errors do not.

## 5. Good / Base / Bad Cases

Good: episode81 returns720P H.264, episode82 returns1080P HEVC; both use their exact Map entries and fully decode. Base: disabled deployments attempt each ordinary source once. Bad: pick any model with matching duration, rotate Android as a priority, assume all successful episodes have the same resolution, or use interactive playback as a readiness requirement.

## 6. Tests Required

- `TestAndroidModelIdentityAndAddress`, `TestAndroidWaitingCancellation`, `TestAndroidQueueDoesNotConsumeExecutionTimeout`, `TestAndroidAppVersionDiagnostics`, `TestAndroidWaitsForPackageService`: wrong identity/envelope, main-vs-fork PID selection, private control validation, canceled waiter ownership.
- `node scripts/check-hongguo-android.mjs`: exact native RPC payload, numeric/string status0, canonical-vs-numeric ID and fixed error output.
- `TestHongGuoDownloadAndroidLastAttempt`: three ordinary sources once and optional fourth Android attempt.
- `TestHongGuoDownloadAndroidPipeline` with isolated PostgreSQL and `-race`: third failure, checkpointed verification failure, final Android failure, stop/resume budget and local IO failure. Require actual subtests in Go output, not only a package PASS.
- Opt-in `TestDownloadAndroidLive`: active timeout then consecutive81/82, exact episode81 official SHA256 and full audio/video decode. `TestHongGuoDownloadAndroidPipelineLive`: real fourth-source transfer, separate full verification and publication using isolated database and auto-cleaned files.
- Validate optional image with Alpine adb and non-root execution, three Compose bases, persistent-data restart and remote process/script cleanup. Long-term stability and all episode availability need separate observation.

## 7. Wrong vs Correct

Wrong: infer "pure API solved" from App RPC success, or treat a hand-captured model as automated queue completion. Correct: distinguish App runtime dependency from direct HTTP reproduction, and verify the actual controller plus persisted queue, decoding and publication.
