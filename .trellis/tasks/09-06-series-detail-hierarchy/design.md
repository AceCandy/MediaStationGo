# Design

Extend the authenticated Web API with a visible-media-scoped Series projection, reusing canonical search presentation and favorite identity storage. Exact library Series filtering resolves deep links without fetching every poster page. Episode inventory remains complete for administrative actions; UI collapses only logical Episode identities.

Reuse movie backdrop, poster, metadata, tracks and file panels. Hide technical/file fields on the Series summary. Key the current-file panel by concrete media ID so requests, dialogs and playback cannot retain an older selection. Query parameters own navigation; invalid defaults normalize with replace.

Keep existing movie behavior unchanged. The Series version selector explicitly selects a playback file; audio/video/subtitle controls remain informational. Resume must use complete Series-scoped history, not a recent-history sample.
