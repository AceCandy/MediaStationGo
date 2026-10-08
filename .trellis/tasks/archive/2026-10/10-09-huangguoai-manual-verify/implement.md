# Implementation

1. Add candidate persistence and worker transition; verify strict failures retain candidates without publication.
2. Add gated preview/accept and retry/cancel cleanup; test digest, collision, Range and role/profile boundaries.
3. Update HuangGuo-only UI/types/counts and shared labels without offering review on HongGuo.
4. Run isolated PostgreSQL service/handler regressions, Go race/vet, Web lint/build and synthetic browser checks. Independently review the final diff and synchronize spec. Do not deploy or touch existing core artifact.
