export interface Media {
  id: string
  metadata_id?: string
  library_id: string
  library_root_id?: string
  library_name?: string
  library_path?: string
  display_library_id?: string
  display_library_name?: string
  display_library_path?: string
  series_id?: string
	series_title?: string
	title: string
	original_name?: string
  path: string
  relative_path?: string
  size_bytes: number
  duration_sec: number
  width: number
  height: number
  video_codec?: string
  audio_codec?: string
  container?: string
  poster_url?: string
  backdrop_url?: string
  overview?: string
  rating: number
  year: number
  release_date?: string
  season_num: number
  episode_num: number
  scrape_status: string
  tmdb_id: number
  bangumi_id: number
  douban_id?: string
  thetvdb_id?: string
  languages?: string
  countries?: string
  genres?: string
  nsfw: boolean
  metadata_kind?: string
  tmdb_snapshot?: boolean
  douban_snapshot?: boolean
  tmdb_status?: 'missing' | 'partial' | 'complete'
  douban_status?: 'missing' | 'partial' | 'degraded' | 'complete'
  series_tmdb_id?: number
  strm_url?: string
  file_hash?: string
  file_id?: string
  is_duplicate?: boolean
  duplicate_of?: string
  tracks?: MediaTrack[]
  versions?: Media[]
  created_at: string
  updated_at: string
}

export interface MediaCredit {
  person_id: string
  name: string
  role?: string
  type: string
  profile_url?: string
}

export interface MediaTrack {
  index: number
  type: 'video' | 'audio' | 'subtitle'
  codec?: string
  profile?: string
  level?: number
  time_base?: string
  language?: string
  display_language?: string
  title?: string
  display_title?: string
  bit_rate?: number
  is_default: boolean
  is_forced: boolean
  is_hearing_impaired?: boolean
  is_visual_impaired?: boolean
  width?: number
  height?: number
  aspect_ratio?: string
  pixel_format?: string
  bit_depth?: number
  color_range?: string
  color_space?: string
  color_transfer?: string
  color_primaries?: string
  video_range?: string
  average_frame_rate?: number
  real_frame_rate?: number
  channels?: number
  sample_rate?: number
  channel_layout?: string
  sample_format?: string
  bits_per_sample?: number
  is_text_subtitle?: boolean
}

export interface Playlist {
  id: string
  user_id: string
  name: string
  is_public: boolean
  created_at: string
  updated_at: string
}
