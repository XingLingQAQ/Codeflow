package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/codeflow/backend/internal/dbx"
)

// SQLiteService is the durable implementation of the user-facing memory API.
// Atomic/vector memory has its own specialized store; this service persists
// the lifecycle and pagination model exposed by /memory/items.
type SQLiteService struct {
	db *sql.DB
	mu sync.RWMutex
}

func NewSQLiteService(dbPath string) (*SQLiteService, error) {
	if dbPath == "" { return nil, errors.New("memory db path is required") }
	if dbPath != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil { return nil, fmt.Errorf("create memory db dir: %w", err) }
	}
	var db *sql.DB
	var err error
	if dbPath == ":memory:" {
		db, err = dbx.Open(":memory:")
	} else {
		db, err = dbx.Open(dbPath, dbx.WithSynchronous("NORMAL"), dbx.WithMaxOpenConns(1))
	}
	if err != nil { return nil, fmt.Errorf("open memory db: %w", err) }
	if _, err := db.Exec(`
		PRAGMA foreign_keys = ON;
		CREATE TABLE IF NOT EXISTS memory_items (
			id TEXT PRIMARY KEY,
			content TEXT NOT NULL,
			type TEXT NOT NULL CHECK (type IN ('stm', 'ltm')),
			status TEXT NOT NULL CHECK (status IN ('active', 'archived', 'pending_archive', 'pending_delete')),
			session_id TEXT NOT NULL,
			message_index INTEGER NOT NULL,
			timestamp INTEGER NOT NULL,
			heat REAL NOT NULL,
			surprise REAL NOT NULL,
			tags_json TEXT NOT NULL DEFAULT '[]',
			source TEXT NOT NULL,
			is_permanent INTEGER NOT NULL DEFAULT 0,
			archived_at INTEGER
		);
		CREATE INDEX IF NOT EXISTS idx_memory_items_session_time ON memory_items(session_id, timestamp DESC, id DESC);
		CREATE INDEX IF NOT EXISTS idx_memory_items_status ON memory_items(status);
	`); err != nil { _ = db.Close(); return nil, fmt.Errorf("init memory schema: %w", err) }
	var quick string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&quick); err != nil || quick != "ok" {
		_ = db.Close(); if err == nil { err = fmt.Errorf("quick_check returned %q", quick) }; return nil, fmt.Errorf("memory integrity check failed: %w", err)
	}
	// Validate all serialized fields during startup so malformed rows fail closed.
	rows, err := db.Query("SELECT tags_json FROM memory_items")
	if err != nil { _ = db.Close(); return nil, fmt.Errorf("validate memory rows: %w", err) }
	for rows.Next() { var raw string; if err := rows.Scan(&raw); err != nil { rows.Close(); _ = db.Close(); return nil, err }; var tags []string; if err := json.Unmarshal([]byte(raw), &tags); err != nil { rows.Close(); _ = db.Close(); return nil, fmt.Errorf("decode memory tags: %w", err) } }
	if err := rows.Err(); err != nil { rows.Close(); _ = db.Close(); return nil, err }; rows.Close()
	return &SQLiteService{db: db}, nil
}

func (s *SQLiteService) List(ctx context.Context, opts *MemoryListOptions) (*MemoryListResponse, error) {
	if opts == nil { opts = &MemoryListOptions{} }
	where := []string{"1=1"}; args := []interface{}{}
	if opts.Type != "" { where = append(where, "type = ?"); args = append(args, string(opts.Type)) }
	if opts.SessionID != "" { where = append(where, "session_id = ?"); args = append(args, opts.SessionID) }
	if opts.Status != "" { where = append(where, "status = ?"); args = append(args, opts.Status) }
	sortBy := "timestamp"; if opts.SortBy == "heat" || opts.SortBy == "surprise" { sortBy = opts.SortBy }
	order := "DESC"; if strings.EqualFold(opts.SortOrder, "asc") { order = "ASC" }
	limit := opts.Limit; if limit <= 0 { limit = 50 }; offset := opts.Offset; if offset < 0 { offset = 0 }
	base := " FROM memory_items WHERE " + strings.Join(where, " AND ")
	var total int; if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*)"+base, args...).Scan(&total); err != nil { return nil, fmt.Errorf("count memory items: %w", err) }
	rows, err := s.db.QueryContext(ctx, "SELECT id, content, type, status, session_id, message_index, timestamp, heat, surprise, tags_json, source, is_permanent, archived_at"+base+" ORDER BY "+sortBy+" "+order+", id "+order+" LIMIT ? OFFSET ?", append(args, limit, offset)...)
	if err != nil { return nil, fmt.Errorf("list memory items: %w", err) }; defer rows.Close()
	items := make([]MemoryItem, 0)
	for rows.Next() { item, err := scanMemoryItem(rows); if err != nil { return nil, err }; items = append(items, item) }
	if err := rows.Err(); err != nil { return nil, err }
	return &MemoryListResponse{Items: items, Total: total, HasMore: offset+len(items) < total, NextOffset: func() int { if offset+len(items) < total { return offset+len(items) }; return 0 }()}, nil
}

func (s *SQLiteService) Get(ctx context.Context, id string) (*MemoryItem, error) {
	row := s.db.QueryRowContext(ctx, "SELECT id, content, type, status, session_id, message_index, timestamp, heat, surprise, tags_json, source, is_permanent, archived_at FROM memory_items WHERE id = ?", id)
	item, err := scanMemoryItem(row); if errors.Is(err, sql.ErrNoRows) { return nil, nil }; if err != nil { return nil, err }; return &item, nil
}

func (s *SQLiteService) Create(ctx context.Context, req *MemoryItemCreateRequest) (*MemoryItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req == nil || strings.TrimSpace(req.Content) == "" { return nil, errors.New("content is required") }
	typ := req.Type; if typ == "" { typ = MemoryTypeSTM }; now := time.Now().Unix(); id := uuid.NewString(); tags, err := json.Marshal(req.Tags); if err != nil { return nil, err }
	var index int; if err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(message_index), -1) + 1 FROM memory_items WHERE session_id = ?", req.SessionID).Scan(&index); err != nil { return nil, err }
	item := &MemoryItem{ID:id, Content:req.Content, Type:typ, Status:MemoryStatusActive, SessionID:req.SessionID, MessageIndex:index, Timestamp:now, Heat:1, Surprise:.5, Tags:req.Tags, Source:req.Source, IsPermanent:req.IsPermanent}
	_, err = s.db.ExecContext(ctx, "INSERT INTO memory_items (id, content, type, status, session_id, message_index, timestamp, heat, surprise, tags_json, source, is_permanent) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", item.ID,item.Content,item.Type,item.Status,item.SessionID,item.MessageIndex,item.Timestamp,item.Heat,item.Surprise,string(tags),item.Source,boolInt(item.IsPermanent)); if err != nil { return nil, err }; return item,nil
}

func (s *SQLiteService) Update(ctx context.Context, id string, req *MemoryItemUpdateRequest) (*MemoryItem, error) {
	item, err := s.Get(ctx,id); if err != nil || item == nil { return item, err }; if req == nil { return item,nil }
	if req.Tags != nil { raw, e := json.Marshal(*req.Tags); if e != nil { return nil,e }; if _,e=s.db.ExecContext(ctx,"UPDATE memory_items SET tags_json = ? WHERE id = ?",string(raw),id);e!=nil{return nil,e}; item.Tags=*req.Tags }
	if req.IsPermanent != nil { if _,e:=s.db.ExecContext(ctx,"UPDATE memory_items SET is_permanent = ? WHERE id = ?",boolInt(*req.IsPermanent),id);e!=nil{return nil,e};item.IsPermanent=*req.IsPermanent }; return item,nil
}

func (s *SQLiteService) Delete(ctx context.Context, id string) error { _,err:=s.db.ExecContext(ctx,"DELETE FROM memory_items WHERE id = ?",id);return err }

func (s *SQLiteService) Archive(ctx context.Context, id string) (*MemoryItem,error) { now:=time.Now().Unix(); r,err:=s.db.ExecContext(ctx,"UPDATE memory_items SET type = ?, status = ?, archived_at = ? WHERE id = ?",MemoryTypeLTM,MemoryStatusArchived,now,id);if err!=nil{return nil,err};n,_:=r.RowsAffected();if n==0{return nil,nil};return s.Get(ctx,id) }
func (s *SQLiteService) Restore(ctx context.Context, id string) (*MemoryItem,error) { r,err:=s.db.ExecContext(ctx,"UPDATE memory_items SET type = ?, status = ?, archived_at = NULL, heat = 1 WHERE id = ?",MemoryTypeSTM,MemoryStatusActive,id);if err!=nil{return nil,err};n,_:=r.RowsAffected();if n==0{return nil,nil};return s.Get(ctx,id) }
func (s *SQLiteService) RefreshHeatScores(ctx context.Context) error { rows,err:=s.db.QueryContext(ctx,"SELECT id,timestamp FROM memory_items");if err!=nil{return err};type pair struct{id string;timestamp int64};var items []pair;for rows.Next(){var p pair;if err:=rows.Scan(&p.id,&p.timestamp);err!=nil{rows.Close();return err};items=append(items,p)};if err:=rows.Err();err!=nil{rows.Close();return err};if err:=rows.Close();err!=nil{return err};tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return err};defer tx.Rollback();for _,p:=range items{if _,err:=tx.ExecContext(ctx,"UPDATE memory_items SET heat = ? WHERE id = ?",CalculateHeat(p.timestamp,0,.5),p.id);err!=nil{return err}};return tx.Commit() }

func (s *SQLiteService) ReplaceItems(ctx context.Context, sessionID string, items []MemoryItem) error {
	tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return err};defer tx.Rollback();if strings.TrimSpace(sessionID)==""{if _,err=tx.ExecContext(ctx,"DELETE FROM memory_items");err!=nil{return err}}else if _,err=tx.ExecContext(ctx,"DELETE FROM memory_items WHERE session_id = ?",sessionID);err!=nil{return err};stmt,err:=tx.PrepareContext(ctx,"INSERT INTO memory_items (id,content,type,status,session_id,message_index,timestamp,heat,surprise,tags_json,source,is_permanent,archived_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)");if err!=nil{return err};defer stmt.Close();for i:=range items{item:=items[i];if item.ID==""{item.ID=uuid.NewString()};if sessionID!=""{item.SessionID=sessionID};if item.Type==""{item.Type=MemoryTypeSTM};if item.Status==""{item.Status=MemoryStatusActive};if item.Tags==nil{item.Tags=[]string{}};raw,_:=json.Marshal(item.Tags);var archived interface{};if item.ArchivedAt!=nil{archived=*item.ArchivedAt};if _,err:=stmt.ExecContext(ctx,item.ID,item.Content,item.Type,item.Status,item.SessionID,item.MessageIndex,item.Timestamp,item.Heat,item.Surprise,string(raw),item.Source,boolInt(item.IsPermanent),archived);err!=nil{return err}};return tx.Commit()
}

func (s *SQLiteService) Close() error { s.mu.Lock(); defer s.mu.Unlock(); if s.db != nil { err:=s.db.Close(); s.db=nil; return err }; return nil }

type memoryScanner interface { Scan(...interface{}) error }
func scanMemoryItem(scan memoryScanner) (MemoryItem,error) { var item MemoryItem;var typ,status,source,tags string;var permanent int;var archived sql.NullInt64;err:=scan.Scan(&item.ID,&item.Content,&typ,&status,&item.SessionID,&item.MessageIndex,&item.Timestamp,&item.Heat,&item.Surprise,&tags,&source,&permanent,&archived);if err!=nil{return item,err};if err:=json.Unmarshal([]byte(tags),&item.Tags);err!=nil{return item,fmt.Errorf("decode memory tags: %w",err)};item.Type=MemoryType(typ);item.Status=MemoryStatus(status);item.Source=SourceType(source);item.IsPermanent=permanent!=0;if archived.Valid{v:=archived.Int64;item.ArchivedAt=&v};return item,nil }
func boolInt(v bool) int { if v{return 1};return 0 }

// userVersionMigration 是一个由 PRAGMA user_version 记录的增量迁移步骤。
// 语句集在单个事务内执行，版本号同事务落库，崩溃不会留下"已记录未应用"
// 的半截状态。只允许 additive 语句（新表、带默认值的新列）。
type userVersionMigration struct {
	version    int
	statements []string
}

// migrateUserVersion 按版本升序应用所有比当前 user_version 新的迁移。
// 空库、旧库与已是最新的库都收敛到同一状态：版本门控保证不重复加列，
// 重入不报错。既有行在 ADD COLUMN DEFAULT 下保留原值与主键。
func migrateUserVersion(ctx context.Context, db *sql.DB, migrations []userVersionMigration) error {
	if db == nil {
		return errors.New("migrate user version: db is nil")
	}
	ordered := append([]userVersionMigration(nil), migrations...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].version < ordered[j].version })
	var current int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}
	for _, m := range ordered {
		if m.version <= 0 {
			return fmt.Errorf("invalid migration version %d", m.version)
		}
		if m.version <= current {
			continue
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", m.version, err)
		}
		for _, stmt := range m.statements {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("apply migration %d: %w", m.version, err)
			}
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("stamp user_version %d: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("commit migration %d: %w", m.version, err)
		}
		current = m.version
	}
	return nil
}

var (
	defaultMemoryService IMemoryService
	defaultMemoryMu sync.RWMutex
)

func GetMemoryService() IMemoryService { defaultMemoryMu.RLock(); defer defaultMemoryMu.RUnlock(); return defaultMemoryService }
func SetMemoryService(svc IMemoryService) { defaultMemoryMu.Lock(); defer defaultMemoryMu.Unlock(); defaultMemoryService=svc }
func HasMemoryService() bool { defaultMemoryMu.RLock(); defer defaultMemoryMu.RUnlock(); return defaultMemoryService!=nil }
