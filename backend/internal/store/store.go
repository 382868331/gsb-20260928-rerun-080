package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"chainresearch/internal/model"
)

// DefaultStudy 是未指定研究线时使用的默认研究线。
const DefaultStudy = "main"

// studyState 是一条研究线的全部状态：纳入的区块集合、确认检查点、
// 冲突裁决与历史版本快照。原研究线在合并后保持可回看。
type studyState struct {
	meta        model.Study
	blockHashes map[string]bool
	checkpoint  *model.Checkpoint
	resolutions map[string]string
	snapshots   []model.VersionSnapshot
}

// Store 保存所有已导入的原始区块与每条研究线的历史版本快照。
// 区块按 Hash 去重（区块归属按实际区块身份判断），
// 导入中断后重放同一批数据不会产生重复索引。
type Store struct {
	mu         sync.RWMutex
	path       string
	blocks     map[string]model.Block
	studies    map[string]*studyState
	studyOrder []string
}

type persisted struct {
	Blocks  []model.Block    `json:"blocks"`
	Studies []persistedStudy `json:"studies"`
	// 以下为旧版单研究线格式，载入时迁移到默认研究线。
	Snapshots   []model.VersionSnapshot `json:"snapshots,omitempty"`
	Checkpoints []model.Checkpoint      `json:"checkpoints,omitempty"`
	Resolutions map[string]string       `json:"resolutions,omitempty"`
}

type persistedStudy struct {
	Meta        model.Study             `json:"meta"`
	BlockHashes []string                `json:"block_hashes"`
	Checkpoint  *model.Checkpoint       `json:"checkpoint,omitempty"`
	Resolutions map[string]string       `json:"resolutions,omitempty"`
	Snapshots   []model.VersionSnapshot `json:"snapshots"`
}

func Open(dir string) (*Store, error) {
	s := &Store{
		path:    filepath.Join(dir, "store.json"),
		blocks:  map[string]model.Block{},
		studies: map[string]*studyState{},
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
		for _, ps := range p.Studies {
			st := newStudyState(ps.Meta)
			for _, h := range ps.BlockHashes {
				st.blockHashes[h] = true
			}
			st.checkpoint = ps.Checkpoint
			if ps.Resolutions != nil {
				st.resolutions = ps.Resolutions
			}
			st.snapshots = ps.Snapshots
			s.studies[ps.Meta.ID] = st
			s.studyOrder = append(s.studyOrder, ps.Meta.ID)
		}
		// 旧版格式迁移：顶层快照/检查点/裁决归入默认研究线。
		if len(p.Snapshots) > 0 || len(p.Checkpoints) > 0 {
			st := s.ensureStudyLocked(DefaultStudy, "默认研究线")
			st.snapshots = p.Snapshots
			if len(p.Checkpoints) > 0 {
				cp := p.Checkpoints[len(p.Checkpoints)-1]
				st.checkpoint = &cp
			}
			if p.Resolutions != nil {
				st.resolutions = p.Resolutions
			}
			for h := range s.blocks {
				st.blockHashes[h] = true
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return s, nil
}

func newStudyState(meta model.Study) *studyState {
	return &studyState{
		meta:        meta,
		blockHashes: map[string]bool{},
		resolutions: map[string]string{},
	}
}

// ensureStudyLocked 返回研究线状态，不存在则创建（保持追加顺序）。
func (s *Store) ensureStudyLocked(id, name string) *studyState {
	if st, ok := s.studies[id]; ok {
		return st
	}
	if name == "" {
		name = id
	}
	st := newStudyState(model.Study{
		ID:        id,
		Name:      name,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Decisions: []model.Decision{},
	})
	s.studies[id] = st
	s.studyOrder = append(s.studyOrder, id)
	return st
}

// CreateStudy 显式创建一条空研究线（可记录合并来源）。
func (s *Store) CreateStudy(id, name string, parents []string, origin string) (model.Study, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		return model.Study{}, fmt.Errorf("研究线缺少 id")
	}
	if _, ok := s.studies[id]; ok {
		return model.Study{}, fmt.Errorf("研究线 %s 已存在", id)
	}
	st := s.ensureStudyLocked(id, name)
	st.meta.Parents = parents
	st.meta.Origin = origin
	if err := s.flushLocked(); err != nil {
		return model.Study{}, err
	}
	return st.meta, nil
}

// Studies 按创建顺序返回全部研究线元数据。
func (s *Store) Studies() []model.Study {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Study, 0, len(s.studyOrder))
	for _, id := range s.studyOrder {
		out = append(out, s.studies[id].meta)
	}
	return out
}

// StudyMeta 返回单条研究线元数据。
func (s *Store) StudyMeta(id string) (model.Study, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.studies[id]
	if !ok {
		return model.Study{}, false
	}
	return st.meta, true
}

// AppendDecision 向研究线追加一条采用决定记录并落盘，
// 保存重开后可复核每个采用决定来自哪次比较。
func (s *Store) AppendDecision(studyID string, d model.Decision) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.studies[studyID]
	if !ok {
		return fmt.Errorf("研究线 %s 不存在", studyID)
	}
	st.meta.Decisions = append(st.meta.Decisions, d)
	return s.flushLocked()
}

// ImportResult 报告本次导入的实际变化。
type ImportResult struct {
	Added   int
	Skipped int
	// StudyAdded 是本次新纳入目标研究线的区块数（区块可能早已
	// 被其他研究线索引：全局无新增，但该研究线需要重建版本）。
	StudyAdded int
}

// AddBlocks 幂等导入：已存在且内容一致的区块跳过；
// 同 Hash 不同内容视为冲突并拒绝（事务性：全部不写入）。
// 导入到默认研究线。
func (s *Store) AddBlocks(incoming []model.Block) (ImportResult, error) {
	return s.AddBlocksTo(DefaultStudy, incoming)
}

// AddBlocksTo 幂等导入到指定研究线：新区块同时登记进该研究线的
// 区块集合；同一区块（按 Hash 身份）可被多条研究线共有。
func (s *Store) AddBlocksTo(studyID string, incoming []model.Block) (ImportResult, error) {
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
	st := s.ensureStudyLocked(studyID, "")
	changed := result.Added > 0
	for _, b := range incoming {
		if !st.blockHashes[b.Hash] {
			st.blockHashes[b.Hash] = true
			result.StudyAdded++
			changed = true
		}
	}
	if changed {
		if err := s.flushLocked(); err != nil {
			return result, err
		}
	}
	return result, nil
}

// SaveSnapshot 追加一个新版本快照并落盘（默认研究线）。
func (s *Store) SaveSnapshot(snap model.VersionSnapshot) error {
	return s.SaveSnapshotTo(DefaultStudy, snap)
}

// SaveSnapshotTo 向指定研究线追加一个不可变版本快照并落盘。
func (s *Store) SaveSnapshotTo(studyID string, snap model.VersionSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.ensureStudyLocked(studyID, "")
	snap.Study = studyID
	st.snapshots = append(st.snapshots, snap)
	return s.flushLocked()
}

func (s *Store) Snapshot(version int64) (model.VersionSnapshot, bool) {
	return s.SnapshotOf(DefaultStudy, version)
}

// SnapshotOf 返回指定研究线的某个历史版本快照。
func (s *Store) SnapshotOf(studyID string, version int64) (model.VersionSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.studies[studyID]
	if !ok {
		return model.VersionSnapshot{}, false
	}
	for _, snap := range st.snapshots {
		if snap.Version == version {
			return snap, true
		}
	}
	return model.VersionSnapshot{}, false
}

func (s *Store) LatestSnapshot() (model.VersionSnapshot, bool) {
	return s.LatestSnapshotOf(DefaultStudy)
}

// LatestSnapshotOf 返回指定研究线的最新版本快照。
func (s *Store) LatestSnapshotOf(studyID string) (model.VersionSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.studies[studyID]
	if !ok || len(st.snapshots) == 0 {
		return model.VersionSnapshot{}, false
	}
	return st.snapshots[len(st.snapshots)-1], true
}

func (s *Store) Versions() []model.VersionSnapshot {
	return s.VersionsOf(DefaultStudy)
}

// VersionsOf 返回指定研究线的全部历史版本。
func (s *Store) VersionsOf(studyID string) []model.VersionSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.studies[studyID]
	if !ok {
		return nil
	}
	out := make([]model.VersionSnapshot, len(st.snapshots))
	copy(out, st.snapshots)
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

// StudyBlocks 返回指定研究线纳入的区块（按 Hash 排序，确定可复现）。
func (s *Store) StudyBlocks(studyID string) ([]model.Block, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.studies[studyID]
	if !ok {
		return nil, fmt.Errorf("研究线 %s 不存在", studyID)
	}
	out := make([]model.Block, 0, len(st.blockHashes))
	for h := range st.blockHashes {
		if b, ok := s.blocks[h]; ok {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hash < out[j].Hash })
	return out, nil
}

// StudyBlockHashes 返回指定研究线的区块 Hash 集合（用于比较/合并）。
func (s *Store) StudyBlockHashes(studyID string) (map[string]bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.studies[studyID]
	if !ok {
		return nil, fmt.Errorf("研究线 %s 不存在", studyID)
	}
	out := make(map[string]bool, len(st.blockHashes))
	for h := range st.blockHashes {
		out[h] = true
	}
	return out, nil
}

// AddCheckpoint 确认默认研究线的链检查点。
func (s *Store) AddCheckpoint(hash, note, confirmedAt string) error {
	return s.AddCheckpointTo(DefaultStudy, hash, note, confirmedAt)
}

// AddCheckpointTo 确认指定研究线的链检查点：区块必须已纳入该研究线；
// 同一区块重复确认幂等（更新备注与时间）。每条研究线的确认决定
// 相互独立，不因为另一条线更长就自动撤销。
func (s *Store) AddCheckpointTo(studyID, hash, note, confirmedAt string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.studies[studyID]
	if !ok {
		return fmt.Errorf("研究线 %s 不存在", studyID)
	}
	if !st.blockHashes[hash] {
		return fmt.Errorf("区块 %s 尚未纳入研究线 %s，无法确认为检查点", hash, studyID)
	}
	st.checkpoint = &model.Checkpoint{
		Hash:        hash,
		Note:        note,
		ConfirmedAt: confirmedAt,
	}
	return s.flushLocked()
}

func (s *Store) Checkpoints() []model.Checkpoint {
	return s.CheckpointsOf(DefaultStudy)
}

// CheckpointsOf 返回指定研究线当前生效的检查点（0 或 1 个）。
func (s *Store) CheckpointsOf(studyID string) []model.Checkpoint {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.studies[studyID]
	if !ok || st.checkpoint == nil {
		return nil
	}
	return []model.Checkpoint{*st.checkpoint}
}

// ResolveConflict 记录默认研究线研究者对冲突的裁决。
func (s *Store) ResolveConflict(id, action string) error {
	return s.ResolveConflictIn(DefaultStudy, id, action)
}

// ResolveConflictIn 记录指定研究线内对冲突的裁决（如建立独立研究）。
func (s *Store) ResolveConflictIn(studyID, id, action string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.ensureStudyLocked(studyID, "")
	st.resolutions[id] = action
	return s.flushLocked()
}

func (s *Store) Resolutions() map[string]string {
	return s.ResolutionsOf(DefaultStudy)
}

// ResolutionsOf 返回指定研究线的冲突裁决。
func (s *Store) ResolutionsOf(studyID string) map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]string{}
	st, ok := s.studies[studyID]
	if !ok {
		return out
	}
	for k, v := range st.resolutions {
		out[k] = v
	}
	return out
}

// MergeStudy 由两条研究线合并生成新研究线：
// 新线区块集合 = 两线共有区块 ∪ 研究者明确接纳的后续区块；
// 只接纳部分后续资料时，父区块缺失的中间资料保留为待补充范围
// （返回 pendingGaps），不拼接出从未存在的链；
// 相互冲突而未接纳的部分记录为 deferred，不标成资料无效。
// 原有两条线及其确认依据保持不变，可回看。
func (s *Store) MergeStudy(newID, name, aID, bID string, accept []string, keepCheckpoint string, deferred []string) (model.Study, []string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.studies[newID]; ok {
		return model.Study{}, nil, fmt.Errorf("研究线 %s 已存在", newID)
	}
	a, ok := s.studies[aID]
	if !ok {
		return model.Study{}, nil, fmt.Errorf("研究线 %s 不存在", aID)
	}
	b, ok := s.studies[bID]
	if !ok {
		return model.Study{}, nil, fmt.Errorf("研究线 %s 不存在", bID)
	}
	set := map[string]bool{}
	for h := range a.blockHashes {
		if b.blockHashes[h] {
			set[h] = true
		}
	}
	// 接纳的区块必须来自两条来源线之一（按实际区块身份）。
	for _, h := range accept {
		if !a.blockHashes[h] && !b.blockHashes[h] {
			return model.Study{}, nil, fmt.Errorf("接纳的区块 %s 不属于任一来源研究线", h)
		}
		set[h] = true
	}
	st := s.ensureStudyLocked(newID, name)
	st.meta.Parents = []string{aID, bID}
	st.meta.Origin = fmt.Sprintf("合并自 %s 与 %s 的比较", aID, bID)
	// 保留哪一套确认由用户选择；检查点区块必须在新线集合内。
	switch keepCheckpoint {
	case "a":
		st.checkpoint = a.checkpoint
	case "b":
		st.checkpoint = b.checkpoint
	}
	// 选择保留某套确认时，其确认范围（检查点及之前的全部历史）
	// 必须整体进入新线，确认决定才成立；否则该确认不带入新线。
	if st.checkpoint != nil {
		included := true
		for cur := st.checkpoint.Hash; ; {
			blk, ok := s.blocks[cur]
			if !ok {
				included = false
				break
			}
			set[cur] = true
			if blk.PrevHash == "" {
				break
			}
			if _, parentKnown := s.blocks[blk.PrevHash]; !parentKnown {
				break
			}
			cur = blk.PrevHash
		}
		if !included {
			st.checkpoint = nil
		} else {
			cp := *st.checkpoint
			st.checkpoint = &cp
		}
	}
	// 核对父区块依据：缺少的中间资料保留为待补充范围。
	gapSeen := map[string]bool{}
	var gaps []string
	for h := range set {
		blk, ok := s.blocks[h]
		if !ok || blk.PrevHash == "" {
			continue
		}
		if !set[blk.PrevHash] && !gapSeen[blk.PrevHash] {
			gapSeen[blk.PrevHash] = true
			gaps = append(gaps, blk.PrevHash)
		}
	}
	sort.Strings(gaps)
	for h := range set {
		st.blockHashes[h] = true
	}
	if err := s.flushLocked(); err != nil {
		return model.Study{}, nil, err
	}
	_ = deferred // 由调用方记入 Decision，冲突部分保留待决定而非无效。
	return st.meta, gaps, nil
}

// RetractBlock 从研究线撤回一份未确认资料：检查点确认范围内的
// 区块不允许撤回。区块本身仍保留在全局索引中（其他研究线可能
// 仍在引用），只从该研究线的集合中移除。
func (s *Store) RetractBlock(studyID, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.studies[studyID]
	if !ok {
		return fmt.Errorf("研究线 %s 不存在", studyID)
	}
	if !st.blockHashes[hash] {
		return fmt.Errorf("区块 %s 不在研究线 %s 中", hash, studyID)
	}
	if st.checkpoint != nil {
		// 检查点区块及其祖先属于确认范围，不允许撤回。
		if hash == st.checkpoint.Hash || s.descendsFromLocked(st.checkpoint.Hash, hash) {
			return fmt.Errorf("区块 %s 位于检查点确认范围内，不能撤回", hash)
		}
	}
	delete(st.blockHashes, hash)
	return s.flushLocked()
}

// descendsFromLocked 沿全局区块父链判断 from 是否为 target 的后代。
func (s *Store) descendsFromLocked(from, target string) bool {
	for cur := from; ; {
		if cur == target {
			return true
		}
		b, ok := s.blocks[cur]
		if !ok {
			return false
		}
		if _, parentExists := s.blocks[b.PrevHash]; !parentExists {
			return false
		}
		cur = b.PrevHash
	}
}

func (s *Store) flushLocked() error {
	blocks := make([]model.Block, 0, len(s.blocks))
	for _, b := range s.blocks {
		blocks = append(blocks, b)
	}
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].Hash < blocks[j].Hash })
	studies := make([]persistedStudy, 0, len(s.studyOrder))
	for _, id := range s.studyOrder {
		st := s.studies[id]
		hashes := make([]string, 0, len(st.blockHashes))
		for h := range st.blockHashes {
			hashes = append(hashes, h)
		}
		sort.Strings(hashes)
		studies = append(studies, persistedStudy{
			Meta:        st.meta,
			BlockHashes: hashes,
			Checkpoint:  st.checkpoint,
			Resolutions: st.resolutions,
			Snapshots:   st.snapshots,
		})
	}
	p := persisted{
		Blocks:  blocks,
		Studies: studies,
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
