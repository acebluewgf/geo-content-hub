import { useState } from 'react'
import { Button, Modal, Tag } from 'antd'
import releaseNotes from '../../../internal/release/notes.json'

export default function ReleaseNotes({ compact = false }) {
  const [open, setOpen] = useState(false)

  return (
    <>
      <button
        type="button"
        className="version-trigger"
        aria-label={`查看版本更新，当前版本 v${releaseNotes.version}`}
        aria-haspopup="dialog"
        aria-expanded={open}
        title="查看更新记录"
        onClick={() => setOpen(true)}
      >
        {!compact && '内容仓库 · '}<span className="version-number">v{releaseNotes.version}</span>
      </button>
      <Modal
        title="版本更新"
        open={open}
        onCancel={() => setOpen(false)}
        width={600}
        footer={<Button type="primary" onClick={() => setOpen(false)}>知道了</Button>}
        className="release-notes-modal"
      >
        <p className="release-notes-intro">微功夫 GEO 内容中台的功能更新与修复记录</p>
        <div className="release-notes-list">
          {releaseNotes.releases.map((release) => (
            <section className="release-note" key={release.version} aria-label={`v${release.version} 更新记录`}>
              <div className="release-note-meta">
                <strong>v{release.version}</strong>
                {release.version === releaseNotes.version && <Tag color="blue">当前版本</Tag>}
                <time dateTime={release.date}>{release.date}</time>
              </div>
              <h3>{release.title}</h3>
              <ul>{release.changes.map((change) => <li key={change}>{change}</li>)}</ul>
            </section>
          ))}
        </div>
      </Modal>
    </>
  )
}
