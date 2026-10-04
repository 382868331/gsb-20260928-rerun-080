package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"

	"chainresearch/internal/model"
)

// Store 保存所有已导入的原始区块与历史版本快照。
// 区块按 Hash 去重，导入中断后重放同一批数据不会产生重复索引。
type Store struct {
	mu        sync.RWMutex
	path      string
	blocks    map[string]model.Block
	snapshots []model.VersionSnapshot
	// checkpoints 是研究者明确确认的链检查点（按确认时间追加）。
	checkpoints []model.Checkpoint
	// resolutions 记录冲突裁决：冲突 ID -> "independent"（建立独立研究）。
	resolutions map[string]string
	// lines 是全部研究线（含合并产生的新线；原线保持可回看）。
	lines     map[string]*model.ResearchLine
	lineOrder []string
	// comparisons 是已持久化的研究线比较结果，供采用决定溯源。
	comparisons []model.Comparison
	// lineSnapshots 是各研究线独立的版本序列。
	lineSnapshots map[string][]model.VersionSnapshot
}

type persisted struct {
	Blocks        []model.Block                    `json:"blocks"`
	Snapshots     []model.VersionSnapshot          `json:"snapshots"`
	Checkpoints   []model.Checkpoint               `json:"checkpoints,omitempty"`
	Resolutions   map[string]string                `json:"resolutions,omitempty"`
	Lines         []model.ResearchLine             `json:"lines,omitempty"`
	Comparisons   []model.Comparison               `json:"comparisons,omitempty"`
	LineSnapshots map[string][]model.VersionSnapshot `json:"line_snapshots,omitempty"`
}

func Open(dir string) (*Store, error) {
	s := &Store{
		path:          filepath.Join(dir, "store.json"),
		blocks:        map[string]model.Block{},
		resolutions:   map[string]string{},
		lines:         map[string]*model.ResearchLine{},
		lineSnapshots: map[string][]model.VersionSnapshot{},
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.path)
	if err == nil {
		var p persisted
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("损坏的索引文件: %w", err)
		}
		for _, b := range p.Blocks {
			s.blocks[b.Hash] = b
		}
		s.snapshots = p.Snapshots
		s.checkpoints = p.Checkpoints
		if p.Resolutions != nil {
			s.resolutions = p.Resolutions
		}
		for i := range p.Lines {
			line := p.Lines[i]
			s.lines[line.ID] = &line
			s.lineOrder = append(s.lineOrder, line.ID)
		}
		s.comparisons = p.Comparisons
		if p.LineSnapshots != nil {
			s.lineSnapshots = p.LineSnapshots
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return s, nil
}

// ImportResult 报告本次导入的实际变化。
type ImportResult struct {
	Added   int
	Skipped int
}

// AddBlocks 幂等导入：已存在且内容一致的区块跳过；
// 同 Hash 不同内容视为冲突并拒绝（事务性：全部不写入）。
func (s *Store) AddBlocks(incoming []model.Block) (ImportResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := ImportResult{}
	// 先校验整批，保证失败时不留下部分写入。
	seen := map[string]bool{}
	for _, b := range incoming {
		if b.Hash == "" {
			return result, fmt.Errorf("区块缺少 hash")
		}
		if seen[b.Hash] {
			return result, fmt.Errorf("同批次内区块重复: %s", b.Hash)
		}
		seen[b.Hash] = true
		if existing, ok := s.blocks[b.Hash]; ok {
			if blockDigest(existing) != blockDigest(b) {
				return result, fmt.Errorf("区块 %s 内容与已索引区块冲突", b.Hash)
			}
		}
	}
	for _, b := range incoming {
		if _, ok := s.blocks[b.Hash]; ok {
			result.Skipped++
			continue
		}
		s.blocks[b.Hash] = b
		result.Added++
	}
	if result.Added > 0 {
		if err := s.flushLocked(); err != nil {
			return result, err
		}
	}
	return result, nil
}

// SaveSnapshot 追加一个新版本快照并落盘。
func (s *Store) SaveSnapshot(snap model.VersionSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots = append(s.snapshots, snap)
	return s.flushLocked()
}

func (s *Store) Snapshot(version int64) (model.VersionSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, snap := range s.snapshots {
		if snap.Version == version {
			return snap, true
		}
	}
	return model.VersionSnapshot{}, false
}

func (s *Store) LatestSnapshot() (model.VersionSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.snapshots) == 0 {
		return model.VersionSnapshot{}, false
	}
	return s.snapshots[len(s.snapshots)-1], true
}

func (s *Store) Versions() []model.VersionSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.VersionSnapshot, len(s.snapshots))
	copy(out, s.snapshots)
	return out
}

func (s *Store) AllBlocks() []model.Block {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Block, 0, len(s.blocks))
	for _, b := range s.blocks {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hash < out[j].Hash })
	return out
}

// AddCheckpoint 追加一个研究者确认的链检查点。区块必须已索引；
// 同一区块重复确认幂等（更新备注与时间，不产生重复记录）。
func (s *Store) AddCheckpoint(hash, note, confirmedAt string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.blocks[hash]; !ok {
		return fmt.Errorf("区块 %s 尚未索引，无法确认为检查点", hash)
	}
	for i := range s.checkpoints {
		if s.checkpoints[i].Hash == hash {
			s.checkpoints[i].Note = note
			s.checkpoints[i].ConfirmedAt = confirmedAt
			return s.flushLocked()
		}
	}
	s.checkpoints = append(s.checkpoints, model.Checkpoint{
		Hash:        hash,
		Note:        note,
		ConfirmedAt: confirmedAt,
	})
	return s.flushLocked()
}

func (s *Store) Checkpoints() []model.Checkpoint {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Checkpoint, len(s.checkpoints))
	copy(out, s.checkpoints)
	return out
}

// ResolveConflict 记录研究者对冲突的裁决（如建立独立研究）。
func (s *Store) ResolveConflict(id, action string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolutions[id] = action
	return s.flushLocked()
}

func (s *Store) Resolutions() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.resolutions))
	for k, v := range s.resolutions {
		out[k] = v
	}
	return out
}

// CreateLine 追加一条研究线（ID 由存储分配，保证唯一）并落盘。
func (s *Store) CreateLine(line model.ResearchLine) (model.ResearchLine, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.lineOrder) + 1
	for {
		line.ID = "L" + strconv.Itoa(n)
		if _, exists := s.lines[line.ID]; !exists {
			break
		}
		n++
	}
	stored := line
	s.lines[line.ID] = &stored
	s.lineOrder = append(s.lineOrder, line.ID)
	if err := s.flushLocked(); err != nil {
		return model.ResearchLine{}, err
	}
	return stored, nil
}

// UpdateLine 覆盖保存一条研究线（导入新区块、确认检查点、撤回等）。
func (s *Store) UpdateLine(line *model.ResearchLine) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.lines[line.ID]; !ok {
		return fmt.Errorf("研究线 %s 不存在", line.ID)
	}
	stored := *line
	s.lines[line.ID] = &stored
	return s.flushLocked()
}

func (s *Store) Line(id string) (model.ResearchLine, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	line, ok := s.lines[id]
	if !ok {
		return model.ResearchLine{}, false
	}
	return *line, true
}

func (s *Store) Lines() []model.ResearchLine {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.ResearchLine, 0, len(s.lineOrder))
	for _, id := range s.lineOrder {
		out = append(out, *s.lines[id])
	}
	return out
}

// AddComparison 持久化一次研究线比较结果（ID 由存储分配）。
func (s *Store) AddComparison(cmp model.Comparison) (model.Comparison, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cmp.ID = "C" + strconv.Itoa(len(s.comparisons)+1)
	s.comparisons = append(s.comparisons, cmp)
	if err := s.flushLocked(); err != nil {
		return model.Comparison{}, err
	}
	return cmp, nil
}

func (s *Store) Comparison(id string) (model.Comparison, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.comparisons {
		if c.ID == id {
			return c, true
		}
	}
	return model.Comparison{}, false
}

func (s *Store) Comparisons() []model.Comparison {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Comparison, len(s.comparisons))
	copy(out, s.comparisons)
	return out
}

// SaveLineSnapshot 为研究线追加一个版本快照（研究线有独立版本序列）。
func (s *Store) SaveLineSnapshot(snap model.VersionSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lineSnapshots[snap.LineID] = append(s.lineSnapshots[snap.LineID], snap)
	return s.flushLocked()
}

func (s *Store) LineSnapshot(lineID string, version int64) (model.VersionSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, snap := range s.lineSnapshots[lineID] {
		if snap.Version == version {
			return snap, true
		}
	}
	return model.VersionSnapshot{}, false
}

func (s *Store) LatestLineSnapshot(lineID string) (model.VersionSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snaps := s.lineSnapshots[lineID]
	if len(snaps) == 0 {
		return model.VersionSnapshot{}, false
	}
	return snaps[len(snaps)-1], true
}

func (s *Store) LineVersions(lineID string) []model.VersionSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.VersionSnapshot, len(s.lineSnapshots[lineID]))
	copy(out, s.lineSnapshots[lineID])
	return out
}

func (s *Store) flushLocked() error {
	blocks := make([]model.Block, 0, len(s.blocks))
	for _, b := range s.blocks {
		blocks = append(blocks, b)
	}
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].Hash < blocks[j].Hash })
	p := persisted{
		Blocks:      blocks,
		Snapshots:   s.snapshots,
		Checkpoints: s.checkpoints,
		Resolutions: s.resolutions,
	}
	for _, id := range s.lineOrder {
		p.Lines = append(p.Lines, *s.lines[id])
	}
	p.Comparisons = s.comparisons
	if len(s.lineSnapshots) > 0 {
		p.LineSnapshots = s.lineSnapshots
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// blockDigest 对区块全部业务内容取摘要，用于检测同 Hash 冲突。
func blockDigest(b model.Block) string {
	data, _ := json.Marshal(b)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
