// Package repository is the data-access layer. It is the ONLY layer that knows
// how PII maps to encrypted columns and blind indexes — handlers deal in
// plaintext DTOs, the database deals in ciphertext, and this package translates
// between them.
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dyd/dyd-server/internal/crypto"
	"github.com/dyd/dyd-server/internal/database"
	"github.com/dyd/dyd-server/internal/models"
)

// ErrDuplicate is returned when a unique constraint (e.g. one application per
// phone) is violated. Handlers map it to 409 Conflict.
var ErrDuplicate = errors.New("duplicate")

// ErrNotFound is returned when a lookup matches no row.
var ErrNotFound = errors.New("not found")

type Repository struct {
	db     *database.DB
	cipher *crypto.Cipher
	bidx   *crypto.BlindIndex
	signer *crypto.Signer
	batch  string
}

func New(db *database.DB, cipher *crypto.Cipher, bidx *crypto.BlindIndex, signer *crypto.Signer, currentBatch string) *Repository {
	return &Repository{db: db, cipher: cipher, bidx: bidx, signer: signer, batch: currentBatch}
}

// docCodePrefix is the document-kind prefix baked into every admit-card
// verification code (DYD-AC-XXXXXXXX) and into the HMAC seed. Keeping it a const
// here keeps create/lookup/verify in perfect agreement.
const docCodePrefix = "AC"

// ---- applications -----------------------------------------------------------

// CreateApplication encrypts PII, computes blind indexes, and inserts. Returns
// the new row id. On a duplicate phone it returns ErrDuplicate.
func (r *Repository) CreateApplication(ctx context.Context, in models.ApplicationInput) (string, error) {
	nameEnc, err := r.cipher.Encrypt(in.Name)
	if err != nil {
		return "", err
	}
	phoneEnc, err := r.cipher.Encrypt(in.Phone)
	if err != nil {
		return "", err
	}
	emailEnc, err := r.cipher.Encrypt(in.Email)
	if err != nil {
		return "", err
	}
	dobEnc, err := r.cipher.Encrypt(in.DOB)
	if err != nil {
		return "", err
	}
	addrEnc, err := r.cipher.Encrypt(in.Address)
	if err != nil {
		return "", err
	}
	eduEnc, err := r.cipher.Encrypt(in.Education)
	if err != nil {
		return "", err
	}
	gpaEnc, err := r.cipher.Encrypt(in.GPA)
	if err != nil {
		return "", err
	}
	photoEnc, err := r.cipher.Encrypt(in.PhotoData)
	if err != nil {
		return "", err
	}

	phoneBidx := r.bidx.Hash(in.Phone)
	phoneDOBBidx := r.bidx.Hash(in.Phone + "|" + normalizeDate(in.DOB))

	const q = `
		INSERT INTO applications (
			name_enc, phone_enc, email_enc, dob_enc, address_enc, education_enc,
			gpa_enc, photo_enc, gender, passing_year, computer_skill, own_computer,
			can_attend, division, district, phone_bidx, phone_dob_bidx, batch
		) VALUES (
			$1,$2,$3,$4,$5,$6,
			$7,$8,$9,$10,$11,$12,
			$13,$14,$15,$16,$17,$18
		)
		RETURNING id`

	var id string
	err = r.db.Primary.QueryRow(ctx, q,
		nameEnc, phoneEnc, emailEnc, dobEnc, addrEnc, eduEnc,
		gpaEnc, photoEnc, in.Gender, in.PassingYear, in.ComputerSkill, in.OwnComputer,
		in.CanAttend, in.Division, in.District, phoneBidx, phoneDOBBidx, r.batch,
	).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return "", ErrDuplicate
		}
		return "", fmt.Errorf("insert application: %w", err)
	}

	// Seed the stable verification code on the freshly-minted (immutable) id and
	// persist it, so GET /v1/verify is a single indexed lookup. Best-effort: a
	// failure here must not lose the application (the row already exists), and the
	// code is deterministic so it can be backfilled on the next admit-card read.
	code := r.signer.DocCode(docCodePrefix, id)
	if _, uerr := r.db.Primary.Exec(ctx,
		`UPDATE applications SET verify_code = $1 WHERE id = $2`, code, id); uerr != nil {
		// Log-worthy but non-fatal; FindAdmitCard backfills a missing code.
		return id, nil
	}
	return id, nil
}

// FindAdmitCard looks up an application by phone + DOB (anti-enumeration) and
// returns a decrypted, presentation-ready view. Reads go to the replica.
func (r *Repository) FindAdmitCard(ctx context.Context, in models.AdmitCardLookup) (*models.AdmitCardView, error) {
	bidx := r.bidx.Hash(in.Phone + "|" + normalizeDate(in.DOB))

	const q = `
		SELECT id, name_enc, phone_enc, email_enc, address_enc, education_enc,
		       photo_enc, division, district, batch, roll_no,
		       admission_confirmed, status, created_at, verify_code
		FROM applications
		WHERE phone_dob_bidx = $1
		LIMIT 1`

	row := r.db.Replica.QueryRow(ctx, q, bidx)

	var (
		id, nameEnc, phoneEnc, emailEnc, addrEnc, eduEnc string
		photoEnc, division, district, batch, status      string
		rollNo                                            *string
		confirmed                                         bool
		createdAt                                         time.Time
		verifyCode                                        string
	)
	err := row.Scan(&id, &nameEnc, &phoneEnc, &emailEnc, &addrEnc, &eduEnc,
		&photoEnc, &division, &district, &batch, &rollNo,
		&confirmed, &status, &createdAt, &verifyCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lookup admit card: %w", err)
	}

	// Backfill the verification code for rows created before verify_code existed
	// (or whose create-time UPDATE didn't land). The code is deterministic from
	// the immutable id, so this is idempotent. Written to the primary.
	if verifyCode == "" {
		verifyCode = r.signer.DocCode(docCodePrefix, id)
		_, _ = r.db.Primary.Exec(ctx,
			`UPDATE applications SET verify_code = $1 WHERE id = $2 AND verify_code = ''`,
			verifyCode, id)
	}

	dec := func(enc string) string {
		v, derr := r.cipher.Decrypt(enc)
		if derr != nil {
			return ""
		}
		return v
	}

	view := &models.AdmitCardView{
		ID:                 id,
		Name:               dec(nameEnc),
		Phone:              dec(phoneEnc),
		Email:              dec(emailEnc),
		Address:            dec(addrEnc),
		Education:          dec(eduEnc),
		PhotoURL:           dec(photoEnc),
		Division:           division,
		District:           district,
		Batch:              batch,
		AdmissionConfirmed: confirmed,
		Status:             status,
		ApplicationDate:    createdAt.Format("2006-01-02"),
		VerifyCode:         verifyCode,
	}
	if rollNo != nil {
		view.RollNo = *rollNo
	}
	return view, nil
}

// FindByVerifyCode resolves a verification code to a NON-PII status view. It is
// the backing query for GET /v1/verify: a scanned QR (or a manually-typed code)
// confirms the document is genuine and reports its lifecycle status without ever
// returning who the applicant is. Reads go to the replica. An unknown or empty
// code returns ErrNotFound (handlers turn that into a clean "invalid" result).
func (r *Repository) FindByVerifyCode(ctx context.Context, code string) (*models.VerifyView, error) {
	code = strings.TrimSpace(strings.ToUpper(code))
	if code == "" {
		return nil, ErrNotFound
	}

	const q = `
		SELECT batch, roll_no, admission_confirmed, status, created_at
		FROM applications
		WHERE verify_code = $1
		LIMIT 1`

	var (
		batch, status string
		rollNo        *string
		confirmed     bool
		createdAt     time.Time
	)
	err := r.db.Replica.QueryRow(ctx, q, code).
		Scan(&batch, &rollNo, &confirmed, &status, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("verify code lookup: %w", err)
	}

	view := &models.VerifyView{
		Valid:              true,
		DocType:            "admit-card",
		Status:             status,
		Batch:              batch,
		AdmissionConfirmed: confirmed,
		IssuedDate:         createdAt.Format("2006-01-02"),
	}
	if rollNo != nil {
		view.RollNo = *rollNo
	}
	return view, nil
}

// ---- contact ----------------------------------------------------------------

func (r *Repository) CreateContact(ctx context.Context, in models.ContactInput) error {
	nameEnc, err := r.cipher.Encrypt(in.Name)
	if err != nil {
		return err
	}
	emailEnc, err := r.cipher.Encrypt(in.Email)
	if err != nil {
		return err
	}
	phoneEnc, err := r.cipher.Encrypt(in.Phone)
	if err != nil {
		return err
	}
	msgEnc, err := r.cipher.Encrypt(in.Message)
	if err != nil {
		return err
	}

	const q = `
		INSERT INTO contact_messages (name_enc, email_enc, phone_enc, subject, message_enc)
		VALUES ($1,$2,$3,$4,$5)`
	if _, err := r.db.Primary.Exec(ctx, q, nameEnc, emailEnc, phoneEnc, in.Subject, msgEnc); err != nil {
		return fmt.Errorf("insert contact: %w", err)
	}
	return nil
}

// ---- newsletter -------------------------------------------------------------

// Subscribe stores an encrypted email with a blind index for dedupe. A repeat
// subscription is treated as success (idempotent), not an error.
func (r *Repository) Subscribe(ctx context.Context, in models.NewsletterInput) error {
	emailEnc, err := r.cipher.Encrypt(in.Email)
	if err != nil {
		return err
	}
	emailBidx := r.bidx.Hash(in.Email)

	const q = `
		INSERT INTO newsletter_subscribers (email_enc, email_bidx)
		VALUES ($1,$2)
		ON CONFLICT (email_bidx) DO NOTHING`
	if _, err := r.db.Primary.Exec(ctx, q, emailEnc, emailBidx); err != nil {
		return fmt.Errorf("insert subscriber: %w", err)
	}
	return nil
}

// normalizeDate reduces the accepted date formats to a canonical yyyy-mm-dd so
// the phone+dob blind index is stable regardless of the input format.
func normalizeDate(s string) string {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"2006-01-02", "02-01-2006", "02/01/2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return s
}
