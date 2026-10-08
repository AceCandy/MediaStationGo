# Review confirmation dialog

Replace browser-native confirmation with the existing project ConfirmDialog/confirmAction flow. Retain the strict-validation warning, explicit consent, candidate version payload, themed confirm/cancel buttons and cancel-without-request behavior. Prevent duplicate actions while awaiting confirmation and avoid sending an acceptance after unmount.

Scope: HuangGuo download page and existing synthetic browser regression only. Investigate unfinished-work updates read-only; do not silently change scheduling, download admission, production configuration or the parallel person-search task. Acceptance: real themed dialog appears; cancel sends no request; acceptance submits matching token; Web lint/build and responsive synthetic checks pass.
