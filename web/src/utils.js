import dayjs from 'dayjs'

export const PLATFORMS = {
  wechat: { label: '微信公众号', color: '#07c160' },
  xiaohongshu: { label: '小红书', color: '#ff2442' },
  zhihu: { label: '知乎', color: '#1677ff' },
  csdn: { label: 'CSDN', color: '#fc5531' },
  linkedin: { label: 'LinkedIn', color: '#0a66c2' },
  x: { label: 'X', color: '#111111' },
  facebook: { label: 'Facebook', color: '#1877f2' },
  instagram: { label: 'Instagram', color: '#e4405f' },
  telegram: { label: 'Telegram', color: '#229ed9' },
  wordpress: { label: 'WordPress', color: '#21759b' },
  douyin: { label: '抖音', color: '#fe2c55' },
  bilibili: { label: '哔哩哔哩', color: '#fb7299' },
}

const FALLBACK = ['#5b6b7f', '#8b5cf6', '#0ea5e9', '#f59e0b', '#10b981', '#ef4444']

export function platformMeta(code) {
  const key = (code || '').toLowerCase()
  if (PLATFORMS[key]) return { code: key, ...PLATFORMS[key] }
  let h = 0
  for (let i = 0; i < key.length; i++) h = (h * 31 + key.charCodeAt(i)) >>> 0
  return { code: key, label: code || '未指定', color: FALLBACK[h % FALLBACK.length] }
}

export const ARTICLE_STATUS = {
  draft: { label: '草稿', color: 'default' },
  ready: { label: '待发布', color: 'blue' },
  archived: { label: '已归档', color: 'default' },
}

export const PUBLISH_STATUS = {
  none: { label: '未发布', color: 'default' },
  scheduled: { label: '已排期', color: 'geekblue' },
  queued: { label: '排队中', color: 'gold' },
  publishing: { label: '发布中', color: 'processing' },
  uncertain: { label: '结果待核对', color: 'warning' },
  published: { label: '已发布', color: 'success' },
  failed: { label: '失败', color: 'error' },
  cancelled: { label: '已取消', color: 'default' },
}

export const JOB_STATUS = {
  queued: { label: '排队中', color: 'gold' },
  claimed: { label: '发布中', color: 'processing' },
  uncertain: { label: '结果待核对', color: 'warning' },
  succeeded: { label: '成功', color: 'success' },
  failed_retryable: { label: '可重试失败', color: 'warning' },
  failed_permanent: { label: '永久失败', color: 'error' },
  cancelled: { label: '已取消', color: 'default' },
}

export const REVIEW_STATUS = {
  none: { label: '未审核', color: 'default' },
  pending: { label: '审核中', color: 'gold' },
  approved: { label: '审核通过', color: 'success' },
  rejected: { label: '审核不通过', color: 'error' },
}

export const LANGUAGES = [
  { value: 'zh-CN', label: '简体中文' },
  { value: 'zh-TW', label: '繁体中文' },
  { value: 'en', label: 'English' },
  { value: 'ja', label: '日本語' },
  { value: 'ko', label: '한국어' },
  { value: 'de', label: 'Deutsch' },
  { value: 'fr', label: 'Français' },
  { value: 'es', label: 'Español' },
]

export function fmt(value, pattern = 'YYYY-MM-DD HH:mm') {
  if (!value) return '—'
  return dayjs(value).format(pattern)
}

export function fromNow(value) {
  if (!value) return ''
  const t = dayjs(value)
  const diff = dayjs().diff(t, 'minute')
  if (diff < 1) return '刚刚'
  if (diff < 60) return `${diff} 分钟前`
  if (diff < 60 * 24) return `${Math.floor(diff / 60)} 小时前`
  return t.format('MM-DD HH:mm')
}
