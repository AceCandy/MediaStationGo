const departments: Record<string, string> = { Acting: '表演', Directing: '导演', Writing: '编剧', Production: '制片', Camera: '摄影', Editing: '剪辑', Sound: '声音', Art: '美术', 'Costume & Make-Up': '服装与化妆', 'Visual Effects': '视觉特效', Crew: '其他幕后', Lighting: '灯光', Creator: '创作' }
const statuses: Record<string, string> = { Released: '已上映', Rumored: '传闻中', Planned: '计划中', 'In Production': '制作中', 'Post Production': '后期制作', 'Returning Series': '持续播出', Ended: '已完结', Canceled: '已取消', Pilot: '试播' }
export const tmdbDepartment = (value?: string) => value ? departments[value] || value : ''
export const tmdbStatus = (value?: string) => value ? statuses[value] || value : ''
