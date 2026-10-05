# Design

Split App model validation from existing variant selection without changing normal selection. A typed local error marks only nonempty all-ByteVC2 models. resolveDownloadApp catches that condition and resolves the model's official fallback URL under one bounded compatibility request. Validate HTTPS vas-lf-x.snssdk.com and /video/fplay/1/ ending in the original canonical video ID; reject redirects. Validate the nested fplay response code and video ID, normalize flat variants into the existing App selection format, and decode addresses in memory.

Version-1 URL payload has little-endian magic 0x00a8 and version 1, followed directly by CBC ciphertext. Derive key/IV from SHA512(SHA512(decoded key_seed) || fixed protocol salt), taking the first and second 16-byte slices. The salt and synthetic vectors are recovered from the public ttsdk-ttplayer 1.52.1.13 artifact, verified against its native decoder. The production implementation uses only Go standard crypto.

Retain existing source app identity and error redaction, public-IP transport, highest compatible selection, spade_a decoding, and verification pipeline. Unsupported envelope formats fail safely into the existing worker source rotation. No native SDK is bundled or loaded. Rollback removes the resolver branch and compatibility implementation.
