package eval

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Review 是人工填写的旁路记录；永不修改原始标注，也不授权外部模型调用。
// 身份、独立性和检查内容是填写者的声明，离线校验器不能认证它们的真实性。
type Review struct {
	SchemaVersion   int            `json:"schema_version"`
	SnapshotVersion string         `json:"snapshot_version"`
	SnapshotSHA256  string         `json:"snapshot_sha256"`
	CasesSHA256     string         `json:"cases_sha256"`
	CodeRevision    string         `json:"code_revision"`
	Reviewer        ReviewReviewer `json:"reviewer"`
	Cases           []ReviewEntry  `json:"cases"`
}

type ReviewReviewer struct {
	ID              string `json:"id"`
	Human           bool   `json:"human"`
	Independent     bool   `json:"independent"`
	SnapshotChecked bool   `json:"snapshot_checked"`
}

type ReviewChecks struct {
	Constraints  bool `json:"constraints"`
	Requirements bool `json:"requirements"`
	SKULabels    bool `json:"sku_labels"`
	Feasibility  bool `json:"feasibility"`
	Outcome      bool `json:"outcome"`
	SplitFamily  bool `json:"split_family"`
}

func (c ReviewChecks) complete() bool {
	return c.Constraints && c.Requirements && c.SKULabels && c.Feasibility && c.Outcome && c.SplitFamily
}

type ReviewEntry struct {
	ID         string       `json:"id"`
	CaseSHA256 string       `json:"case_sha256"`
	Decision   string       `json:"decision"` // pending / accepted / changes_requested
	Checks     ReviewChecks `json:"checks"`
	Notes      string       `json:"notes"`
	ReviewedAt string       `json:"reviewed_at"` // 非 pending 时由人填写 RFC3339 时间
}

type ReviewSummary struct {
	Total                   int      `json:"total"`
	Accepted                int      `json:"accepted"`
	Pending                 int      `json:"pending"`
	ChangesRequested        int      `json:"changes_requested"`
	ReviewerDeclared        bool     `json:"reviewer_declared"`
	SnapshotChecked         bool     `json:"snapshot_checked"`
	RecordsComplete         bool     `json:"records_complete"`
	DatasetAnnotationStatus string   `json:"dataset_annotation_status"`
	HumanIdentityVerified   bool     `json:"human_identity_verified"`
	AcceptanceStatus        string   `json:"acceptance_status"`
	PendingIDs              []string `json:"pending_ids"`
	ChangeIDs               []string `json:"change_ids"`
}

var sha256Text = regexp.MustCompile(`^[0-9a-f]{64}$`)

func caseFingerprint(c Case) string {
	data, _ := json.Marshal(c)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func reviewDataset(d Dataset) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if !sha256Text.MatchString(d.SnapshotSHA256) || !sha256Text.MatchString(d.CasesSHA256) {
		return errors.New("review requires dataset hashes from Load")
	}
	return nil
}

func NewReview(d Dataset, revision string) (Review, error) {
	if err := reviewDataset(d); err != nil {
		return Review{}, err
	}
	if revision == "" {
		revision = "unknown"
	}
	r := Review{SchemaVersion: 1, SnapshotVersion: d.Snapshot.Version, SnapshotSHA256: d.SnapshotSHA256,
		CasesSHA256: d.CasesSHA256, CodeRevision: revision, Cases: make([]ReviewEntry, 0, len(d.Cases))}
	for _, c := range d.Cases {
		r.Cases = append(r.Cases, ReviewEntry{ID: c.ID, CaseSHA256: caseFingerprint(c), Decision: "pending"})
	}
	return r, nil
}

func LoadReview(reader io.Reader) (Review, error) {
	data, err := readBounded(reader)
	if err != nil {
		return Review{}, err
	}
	var r Review
	if err := decodeStrict(data, &r); err != nil {
		return Review{}, errors.New("invalid review JSON")
	}
	return r, nil
}

// CheckReview 只核对数据绑定、记录格式和填写进度，不作人工判断或自动批准数据集。
func CheckReview(d Dataset, r Review) (ReviewSummary, error) {
	if err := reviewDataset(d); err != nil {
		return ReviewSummary{}, err
	}
	if r.SchemaVersion != 1 || r.SnapshotVersion != d.Snapshot.Version || r.SnapshotSHA256 != d.SnapshotSHA256 || r.CasesSHA256 != d.CasesSHA256 {
		return ReviewSummary{}, errors.New("review schema or dataset fingerprint differs")
	}
	if len(r.Cases) != len(d.Cases) {
		return ReviewSummary{}, errors.New("review must cover every dataset case exactly once")
	}
	if strings.TrimSpace(r.Reviewer.ID) != r.Reviewer.ID || utf8.RuneCountInString(r.Reviewer.ID) > 128 {
		return ReviewSummary{}, errors.New("reviewer ID must be trimmed and at most 128 characters")
	}
	s := ReviewSummary{Total: len(d.Cases), ReviewerDeclared: r.Reviewer.ID != "" && r.Reviewer.Human && r.Reviewer.Independent,
		SnapshotChecked: r.Reviewer.SnapshotChecked, DatasetAnnotationStatus: d.Snapshot.AnnotationStatus,
		AcceptanceStatus: "not_performed", PendingIDs: []string{}, ChangeIDs: []string{}}
	fingerprints := make(map[string]string, len(d.Cases))
	for _, c := range d.Cases {
		fingerprints[c.ID] = caseFingerprint(c)
	}
	for _, entry := range r.Cases {
		fingerprint, exists := fingerprints[entry.ID]
		if !exists || entry.CaseSHA256 != fingerprint {
			return ReviewSummary{}, errors.New("unknown, duplicate or changed review case")
		}
		delete(fingerprints, entry.ID)
		if utf8.RuneCountInString(entry.Notes) > 2048 {
			return ReviewSummary{}, fmt.Errorf("review notes too long for %s", entry.ID)
		}
		switch entry.Decision {
		case "pending":
			if entry.ReviewedAt != "" {
				return ReviewSummary{}, fmt.Errorf("pending case %s must not have a decision timestamp", entry.ID)
			}
			s.Pending++
			s.PendingIDs = append(s.PendingIDs, entry.ID)
		case "accepted", "changes_requested":
			if r.Reviewer.ID == "" {
				return ReviewSummary{}, errors.New("decided entries require a reviewer ID")
			}
			if _, err := time.Parse(time.RFC3339, entry.ReviewedAt); err != nil {
				return ReviewSummary{}, fmt.Errorf("case %s needs an RFC3339 decision timestamp", entry.ID)
			}
			if entry.Decision == "accepted" {
				if !entry.Checks.complete() {
					return ReviewSummary{}, fmt.Errorf("accepted case %s requires all six checks", entry.ID)
				}
				s.Accepted++
			} else {
				if strings.TrimSpace(entry.Notes) == "" {
					return ReviewSummary{}, fmt.Errorf("case %s needs a reason for requested changes", entry.ID)
				}
				s.ChangesRequested++
				s.ChangeIDs = append(s.ChangeIDs, entry.ID)
			}
		default:
			return ReviewSummary{}, fmt.Errorf("invalid review decision for %s", entry.ID)
		}
	}
	s.RecordsComplete = s.Accepted == s.Total && s.ReviewerDeclared && s.SnapshotChecked
	return s, nil
}
