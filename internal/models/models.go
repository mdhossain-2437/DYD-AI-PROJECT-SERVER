// Package models holds the domain types shared across repositories and
// handlers. Request DTOs carry plaintext (validated at the edge); stored rows
// carry encrypted PII. Conversion happens in the repository layer.
package models

import "time"

// ---- Application ------------------------------------------------------------

// ApplicationInput is the validated inbound payload from POST /v1/applications.
// Field tags drive both JSON binding and go-playground/validator rules.
//
// The categorical answers (gender, computerSkill, ownComputer, canAttend) use
// canonical English tokens so the stored value is language-independent; the
// frontend renders bilingual labels over them. Email is required (the live
// intake form marks it mandatory). The passport photo arrives as a base64
// data-URI in `photoData` and is encrypted at rest like the other PII.
type ApplicationInput struct {
	Name          string `json:"name"          validate:"required,min=2,max=120"`
	Gender        string `json:"gender"        validate:"required,oneof=male female other"`
	Email         string `json:"email"         validate:"required,email,max=160"`
	Phone         string `json:"phone"         validate:"required,bdphone"`
	DOB           string `json:"dob"           validate:"required,datestr"`
	Address       string `json:"address"       validate:"required,min=5,max=400"`
	Division      string `json:"division"      validate:"required,max=40"`
	District      string `json:"district"      validate:"required,max=40"`
	Education     string `json:"education"     validate:"required,max=120"`
	GPA           string `json:"gpa"           validate:"required,max=20"`
	PassingYear   string `json:"passingYear"   validate:"required,numeric,len=4"`
	ComputerSkill string `json:"computerSkill" validate:"required,oneof=none basic intermediate advanced"`
	OwnComputer   string `json:"ownComputer"   validate:"required,oneof=yes no"`
	CanAttend     string `json:"canAttend"     validate:"required,oneof=yes no"`
	// PhotoData is a data:image/...;base64,... URI. Required.
	PhotoData string `json:"photoData" validate:"required,imgdata,max=2500000"`
}

// Application is a stored row (PII already encrypted).
type Application struct {
	ID                 string
	NameEnc            string
	PhoneEnc           string
	EmailEnc           string
	DOBEnc             string
	AddressEnc         string
	EducationEnc       string
	GPAEnc             string
	PhotoEnc           string
	Gender             string
	PassingYear        string
	ComputerSkill      string
	OwnComputer        string
	CanAttend          string
	Division           string
	District           string
	PhoneBIdx          string
	PhoneDOBBIdx       string
	Batch              string
	RollNo             *string
	AdmissionConfirmed bool
	Status             string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// AdmitCardLookup is the anti-enumeration lookup payload: phone alone is not
// enough — the applicant must also supply their date of birth. This raises the
// cost of scraping the admit-card endpoint by phone-number enumeration.
type AdmitCardLookup struct {
	Phone string `json:"phone" validate:"required,bdphone"`
	DOB   string `json:"dob"   validate:"required,datestr"`
}

// AdmitCardView is the decrypted, presentation-ready record returned to the
// applicant after a successful lookup. Mirrors the frontend DocRecord shape.
// PhotoURL carries the decrypted photo as a data-URI (usable directly as an
// <img src>), so no separate media fetch is needed.
type AdmitCardView struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Phone              string `json:"phone"`
	Email              string `json:"email,omitempty"`
	Division           string `json:"division"`
	District           string `json:"district"`
	Address            string `json:"address,omitempty"`
	Education          string `json:"education,omitempty"`
	PhotoURL           string `json:"photoUrl,omitempty"`
	Batch              string `json:"batch,omitempty"`
	RollNo             string `json:"rollNo,omitempty"`
	AdmissionConfirmed bool   `json:"admissionConfirmed"`
	Status             string `json:"status"`
	ApplicationDate    string `json:"applicationDate,omitempty"`
	// VerifyCode is the stable, non-PII code a QR scan resolves to. The client
	// renders it into the admit card's QR so /verify can confirm authenticity.
	VerifyCode string `json:"verifyCode,omitempty"`
}

// VerifyView is the deliberately non-PII result of GET /v1/verify. It confirms a
// code corresponds to a genuine issued document and reports its lifecycle status
// WITHOUT revealing who the applicant is — no name, phone, email, or address.
// A third party scanning a QR learns "this admit card is real and its status is
// X", nothing more.
type VerifyView struct {
	Valid              bool   `json:"valid"`
	DocType            string `json:"docType,omitempty"` // e.g. "admit-card"
	Status             string `json:"status,omitempty"`  // submitted|reviewed|selected|rejected
	Batch              string `json:"batch,omitempty"`
	RollNo             string `json:"rollNo,omitempty"`
	AdmissionConfirmed bool   `json:"admissionConfirmed,omitempty"`
	IssuedDate         string `json:"issuedDate,omitempty"`
}

// ---- Contact & newsletter ---------------------------------------------------

type ContactInput struct {
	Name    string `json:"name"    validate:"required,min=2,max=120"`
	Email   string `json:"email"   validate:"omitempty,email,max=160"`
	Phone   string `json:"phone"   validate:"omitempty,bdphone"`
	Subject string `json:"subject" validate:"omitempty,max=160"`
	Message string `json:"message" validate:"required,min=5,max=2000"`
}

type NewsletterInput struct {
	Email string `json:"email" validate:"required,email,max=160"`
}
