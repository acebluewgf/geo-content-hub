package server

import (
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

type Article struct {
	ID               int64
	Revision         int64
	UID              string
	ClientID         sql.NullInt64
	ExternalID       sql.NullString
	Platform         string
	PlatformAccount  sql.NullString
	Language         string
	Topic            sql.NullString
	TagsJSON         sql.NullString
	Title            string
	Summary          string
	BodyMD           string
	BodyHTML         string
	BodyText         string
	CoverMediaID     sql.NullInt64
	ContentHash      sql.NullString
	WordCount        int
	ReadingTime      int
	Status           string
	PublishStatus    string
	PublishMode      string
	ReviewStatus     string
	ReviewNote       string
	ReviewCheckedAt  sql.NullTime
	ScheduledAt      sql.NullTime
	FirstPublishedAt sql.NullTime
	LastPublishedAt  sql.NullTime
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        sql.NullTime
}

const articleCols = `a.id, a.uid, a.client_id, a.external_id, a.platform, a.platform_account,
	a.language, a.topic, a.tags, a.title, a.summary, a.body_md, a.body_html, a.body_text,
	a.cover_media_id, a.content_hash, a.word_count, a.reading_time, a.status, a.publish_status,
	a.publish_mode, a.review_status, a.review_note, a.review_checked_at,
	a.scheduled_at, a.first_published_at, a.last_published_at, a.created_at, a.updated_at, a.deleted_at, a.revision`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanArticle(sc rowScanner) (*Article, error) {
	var a Article
	var clientID, coverID sql.NullInt64
	var externalID, platformAccount, topic, tags, hash sql.NullString
	var scheduled, firstPub, lastPub, deleted, reviewChecked sql.NullTime
	err := sc.Scan(&a.ID, &a.UID, &clientID, &externalID, &a.Platform, &platformAccount,
		&a.Language, &topic, &tags, &a.Title, &a.Summary, &a.BodyMD, &a.BodyHTML, &a.BodyText,
		&coverID, &hash, &a.WordCount, &a.ReadingTime, &a.Status, &a.PublishStatus,
		&a.PublishMode, &a.ReviewStatus, &a.ReviewNote, &reviewChecked,
		&scheduled, &firstPub, &lastPub, &a.CreatedAt, &a.UpdatedAt, &deleted, &a.Revision)
	if err != nil {
		return nil, err
	}
	a.ClientID, a.ExternalID, a.PlatformAccount = clientID, externalID, platformAccount
	a.Topic, a.TagsJSON, a.CoverMediaID, a.ContentHash = topic, tags, coverID, hash
	a.ReviewCheckedAt = reviewChecked
	a.ScheduledAt, a.FirstPublishedAt, a.LastPublishedAt, a.DeletedAt = scheduled, firstPub, lastPub, deleted
	return &a, nil
}

func (s *Server) articleByUID(uid string) (*Article, error) {
	row := s.db.QueryRow("SELECT "+articleCols+" FROM articles a WHERE a.uid = ? AND a.deleted_at IS NULL"+s.db.lock(), uid)
	a, err := scanArticle(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

func (s *Server) articleByID(id int64) (*Article, error) {
	row := s.db.QueryRow("SELECT "+articleCols+" FROM articles a WHERE a.id = ?"+s.db.lock(), id)
	a, err := scanArticle(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

func (a *Article) tags() []string {
	if !a.TagsJSON.Valid || a.TagsJSON.String == "" {
		return []string{}
	}
	var out []string
	if json.Unmarshal([]byte(a.TagsJSON.String), &out) != nil {
		return []string{}
	}
	return out
}

func (s *Server) insertArticle(a *Article) error {
	var clientID, coverID interface{}
	if a.ClientID.Valid {
		clientID = a.ClientID.Int64
	}
	if a.CoverMediaID.Valid {
		coverID = a.CoverMediaID.Int64
	}
	res, err := s.db.Exec(`INSERT INTO articles
		(uid, client_id, external_id, platform, platform_account, language, topic, tags, title, summary,
		 body_md, body_html, body_text, cover_media_id, content_hash, word_count, reading_time,
		 status, publish_status, publish_mode, scheduled_at, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,UTC_TIMESTAMP(),UTC_TIMESTAMP())`,
		a.UID, clientID, nullable(a.ExternalID), a.Platform, nullable(a.PlatformAccount), a.Language,
		nullable(a.Topic), nullable(a.TagsJSON), a.Title, a.Summary,
		a.BodyMD, a.BodyHTML, a.BodyText, coverID, nullable(a.ContentHash), a.WordCount, a.ReadingTime,
		a.Status, a.PublishStatus, a.PublishMode, nullableTime(a.ScheduledAt))
	if err != nil {
		return err
	}
	a.ID, _ = res.LastInsertId()
	a.Revision = 1
	return nil
}

func (s *Server) updateArticle(a *Article) error {
	if err := s.ensureArticleIdle(a.ID); err != nil {
		return err
	}
	var coverID interface{}
	if a.CoverMediaID.Valid {
		coverID = a.CoverMediaID.Int64
	}
	res, err := s.db.Exec(`UPDATE articles SET
		platform=?, platform_account=?, language=?, topic=?, tags=?, title=?, summary=?,
		body_md=?, body_html=?, body_text=?, cover_media_id=?, content_hash=?, word_count=?, reading_time=?,
		status=?, publish_status=?, publish_mode=?, scheduled_at=?,
		first_published_at=?, last_published_at=?, updated_at=UTC_TIMESTAMP(), revision=revision+1
		WHERE id=? AND revision=?`,
		a.Platform, nullable(a.PlatformAccount), a.Language, nullable(a.Topic), nullable(a.TagsJSON),
		a.Title, a.Summary, a.BodyMD, a.BodyHTML, a.BodyText, coverID, nullable(a.ContentHash),
		a.WordCount, a.ReadingTime, a.Status, a.PublishStatus, a.PublishMode, nullableTime(a.ScheduledAt),
		nullableTime(a.FirstPublishedAt), nullableTime(a.LastPublishedAt), a.ID, a.Revision)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return s.conflict("revision_conflict", "文章已被修改，请重新载入后保存")
	}
	a.Revision++
	return nil
}

func nullable(v sql.NullString) interface{} {
	if v.Valid {
		return v.String
	}
	return nil
}

func nullableTime(v sql.NullTime) interface{} {
	if v.Valid {
		return v.Time.UTC()
	}
	return nil
}

type Job struct {
	Snapshot         sql.NullString
	LeaseToken       string
	LeaseClientID    sql.NullInt64
	ReviewToken      string
	ReviewClientID   sql.NullInt64
	ID               int64
	UID              string
	ArticleID        int64
	Platform         string
	Status           string
	ActiveKey        sql.NullInt64
	ScheduledAt      sql.NullTime
	NextAttemptAt    sql.NullTime
	Attempts         int
	MaxAttempts      int
	LeaseOwner       sql.NullString
	LeaseExpiresAt   sql.NullTime
	LastErrorCode    sql.NullString
	LastErrorMessage sql.NullString
	ResultAccount    sql.NullString
	ResultURL        sql.NullString
	ResultPostID     sql.NullString
	ResultAt         sql.NullTime
	ResultMeta       sql.NullString
	ReviewStatus     string
	ReviewAttempts   int
	ReviewNote       string
	ReviewURL        string
	ReviewCheckedAt  sql.NullTime
	ReviewNextAt     sql.NullTime
	ReviewLeaseOwner string
	ReviewLeaseAt    sql.NullTime
	FinishedAt       sql.NullTime
	CancelReason     sql.NullString
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

const jobCols = `j.id, j.uid, j.article_id, j.platform, j.status, j.active_key, j.scheduled_at,
	j.next_attempt_at, j.attempts, j.max_attempts, j.lease_owner, j.lease_expires_at,
	j.last_error_code, j.last_error_message, j.result_account, j.result_url, j.result_post_id,
	j.result_at, j.result_meta, j.review_status, j.review_attempts, j.review_note, j.review_url,
	j.review_checked_at, j.review_next_at, j.review_lease_owner, j.review_lease_expires_at,
	j.finished_at, j.cancel_reason, j.created_at, j.updated_at,j.content_snapshot,j.lease_token,j.lease_client_id,j.review_lease_token,j.review_client_id`

func scanJob(sc rowScanner) (*Job, error) {
	var j Job
	var active, meta sql.NullString
	var scheduled, next, leaseAt, resultAt, finished, reviewChecked, reviewNext, reviewLeaseAt sql.NullTime
	var leaseOwner, errCode, errMsg, account, url, postID, cancel, reviewOwner sql.NullString
	err := sc.Scan(&j.ID, &j.UID, &j.ArticleID, &j.Platform, &j.Status, &active, &scheduled,
		&next, &j.Attempts, &j.MaxAttempts, &leaseOwner, &leaseAt, &errCode, &errMsg,
		&account, &url, &postID, &resultAt, &meta,
		&j.ReviewStatus, &j.ReviewAttempts, &j.ReviewNote, &j.ReviewURL,
		&reviewChecked, &reviewNext, &reviewOwner, &reviewLeaseAt,
		&finished, &cancel, &j.CreatedAt, &j.UpdatedAt, &j.Snapshot, &j.LeaseToken, &j.LeaseClientID, &j.ReviewToken, &j.ReviewClientID)
	if err != nil {
		return nil, err
	}
	if active.Valid {
		j.ActiveKey = sql.NullInt64{Int64: 1, Valid: true}
	}
	j.ScheduledAt, j.NextAttemptAt, j.LeaseExpiresAt = scheduled, next, leaseAt
	j.LeaseOwner, j.LastErrorCode, j.LastErrorMessage = leaseOwner, errCode, errMsg
	j.ResultAccount, j.ResultURL, j.ResultPostID, j.ResultAt = account, url, postID, resultAt
	j.ResultMeta, j.FinishedAt, j.CancelReason = meta, finished, cancel
	j.ReviewCheckedAt, j.ReviewNextAt, j.ReviewLeaseAt = reviewChecked, reviewNext, reviewLeaseAt
	j.ReviewLeaseOwner = reviewOwner.String
	return &j, nil
}

func (s *Server) jobByUID(uid string) (*Job, error) {
	row := s.db.QueryRow("SELECT "+jobCols+" FROM publish_jobs j WHERE j.uid = ?"+s.db.lock(), uid)
	j, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return j, err
}

func (s *Server) activeJob(articleID int64) (*Job, error) {
	row := s.db.QueryRow("SELECT "+jobCols+` FROM publish_jobs j
		WHERE j.article_id = ? AND j.active_key = 1 LIMIT 1`+s.db.lock(), articleID)
	j, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return j, err
}

func (s *Server) latestJob(articleID int64) (*Job, error) {
	row := s.db.QueryRow("SELECT "+jobCols+` FROM publish_jobs j
		WHERE j.article_id = ? ORDER BY j.id DESC LIMIT 1`, articleID)
	j, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return j, err
}

type MediaRow struct {
	ID     int64
	UID    string
	Mime   string
	Width  int
	Height int
	Size   int
	Path   string
}

func (s *Server) mediaByID(id int64) (*MediaRow, error) {
	var m MediaRow
	err := s.db.QueryRow("SELECT id, uid, mime, width, height, size, path FROM media WHERE id = ? AND status='active'", id).
		Scan(&m.ID, &m.UID, &m.Mime, &m.Width, &m.Height, &m.Size, &m.Path)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Server) mediaByUID(uid string) (*MediaRow, error) {
	var m MediaRow
	err := s.db.QueryRow("SELECT id, uid, mime, width, height, size, path FROM media WHERE uid = ? AND status='active'", uid).
		Scan(&m.ID, &m.UID, &m.Mime, &m.Width, &m.Height, &m.Size, &m.Path)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *MediaRow) URL(base string) string {
	return strings.TrimRight(base, "/") + "/media/" + m.Path
}

func (s *Server) setting(key string) string {
	var raw []byte
	if err := s.db.QueryRow("SELECT setting_value FROM settings WHERE setting_key = ?", key).Scan(&raw); err != nil {
		return ""
	}
	return strings.Trim(string(raw), "\"")
}

func (s *Server) settingInt(key string, def int) int {
	v := s.setting(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return n
}
