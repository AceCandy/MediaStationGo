const departments: Record<string, string> = { Acting: '表演', Directing: '导演', Writing: '编剧', Production: '制片', Camera: '摄影', Editing: '剪辑', Sound: '声音', Art: '美术', 'Costume & Make-Up': '服装与化妆', 'Visual Effects': '视觉特效', Crew: '其他幕后', Lighting: '灯光', Creator: '创作' }
const statuses: Record<string, string> = { Released: '已上映', Rumored: '传闻中', Planned: '计划中', 'In Production': '制作中', 'Post Production': '后期制作', 'Returning Series': '持续播出', Ended: '已完结', Canceled: '已取消', Pilot: '试播' }
export const tmdbDepartment = (value?: string) => value ? departments[value] || value : ''
export const tmdbStatus = (value?: string) => value ? statuses[value] || value : ''

const jobs: Record<string, string> = {
  Director: '导演', Writer: '编剧', Screenplay: '剧本', Story: '故事', Novel: '原著小说', Characters: '原著角色',
  Producer: '制片人', 'Executive Producer': '执行制片人', 'Co-Producer': '联合制片人', 'Associate Producer': '协力制片人',
  'Director of Photography': '摄影指导', Editor: '剪辑', 'Original Music Composer': '原创配乐', Composer: '作曲',
  Casting: '选角', 'Casting Director': '选角导演', 'Production Design': '制作设计', 'Art Direction': '美术指导',
  'Set Decoration': '布景', 'Costume Design': '服装设计', 'Makeup Artist': '化妆师', Hairstylist: '发型师',
  Stunts: '特技', 'Stunt Coordinator': '特技协调', 'Stunt Double': '特技替身', 'Fight Choreographer': '动作设计',
  'Construction Coordinator': '布景搭建协调', 'Foley Artist': '拟音师', 'First Assistant Director': '第一副导演',
  'Second Assistant Director': '第二副导演', 'Sound Designer': '声音设计', 'Sound Editor': '声音剪辑',
  'Sound Mixer': '混音师', 'Sound Re-Recording Mixer': '重录混音师', 'Visual Effects Supervisor': '视效总监',
  'Visual Effects Producer': '视效制片人', 'Special Effects Supervisor': '特效总监', 'Camera Operator': '摄影操作',
  Gaffer: '灯光师', 'Production Manager': '制作经理', 'Location Manager': '外景经理', 'Script Supervisor': '场记',
}
export const tmdbJob = (value?: string) => value ? value.split(' / ').map(job => jobs[job] || job).join(' / ') : ''
