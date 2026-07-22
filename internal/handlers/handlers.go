// Package handlers holds the HTTP handlers for the public API. Handlers are
// thin: they bind + validate input, run bot verification, delegate to the
// repository, and map domain errors to clean JSON responses. They never log
// PII and never leak internal error detail to clients.
package handlers

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"

	"github.com/dyd/dyd-server/internal/data"
	"github.com/dyd/dyd-server/internal/models"
	"github.com/dyd/dyd-server/internal/repository"
	"github.com/dyd/dyd-server/internal/turnstile"
	"github.com/dyd/dyd-server/internal/validate"
)

type Handler struct {
	repo      *repository.Repository
	validator *validate.Validator
	turnstile *turnstile.Verifier
	log       zerolog.Logger
}

func New(repo *repository.Repository, v *validate.Validator, ts *turnstile.Verifier, log zerolog.Logger) *Handler {
	return &Handler{repo: repo, validator: v, turnstile: ts, log: log}
}

// turnstileToken pulls the bot-verification token from the header the frontend
// sends (or a JSON body field as fallback).
func turnstileToken(c *fiber.Ctx) string {
	if t := c.Get("CF-Turnstile-Response"); t != "" {
		return t
	}
	return ""
}

// ---- POST /v1/applications -------------------------------------------------

func (h *Handler) CreateApplication(c *fiber.Ctx) error {
	var in models.ApplicationInput
	if err := c.BodyParser(&in); err != nil {
		return badRequest(c, "malformed request body")
	}

	if err := h.turnstile.Verify(c.Context(), turnstileToken(c), c.IP()); err != nil {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error":   "bot_check_failed",
			"message": "Verification failed. Please retry.",
		})
	}

	if fieldErrs := h.validator.Struct(in); fieldErrs != nil {
		return validationFailed(c, fieldErrs)
	}

	// Guard the cascading geo selection against the authoritative 8-division /
	// 64-district dataset — never trust the client's division/district pair.
	if !data.IsValidPair(in.Division, in.District) {
		return validationFailed(c, map[string]string{
			"district": "district does not belong to the selected division",
		})
	}

	id, err := h.repo.CreateApplication(c.Context(), in)
	if err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"error":   "already_applied",
				"message": "An application with this phone number already exists.",
			})
		}
		h.log.Error().Err(err).Msg("create application failed")
		return internalError(c)
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"ok":            true,
		"applicationId": id,
		"message":       "Application received.",
	})
}

// ---- POST /v1/admit-card/lookup --------------------------------------------

func (h *Handler) LookupAdmitCard(c *fiber.Ctx) error {
	var in models.AdmitCardLookup
	if err := c.BodyParser(&in); err != nil {
		return badRequest(c, "malformed request body")
	}

	if err := h.turnstile.Verify(c.Context(), turnstileToken(c), c.IP()); err != nil {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error":   "bot_check_failed",
			"message": "Verification failed. Please retry.",
		})
	}

	if fieldErrs := h.validator.Struct(in); fieldErrs != nil {
		return validationFailed(c, fieldErrs)
	}

	view, err := h.repo.FindAdmitCard(c.Context(), in)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			// Deliberately generic: do not reveal whether the phone or the DOB
			// was the mismatch (anti-enumeration).
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error":   "not_found",
				"message": "No matching application found. Check your phone number and date of birth.",
			})
		}
		h.log.Error().Err(err).Msg("admit-card lookup failed")
		return internalError(c)
	}

	return c.JSON(fiber.Map{"ok": true, "record": view})
}

// ---- POST /v1/contact ------------------------------------------------------

func (h *Handler) CreateContact(c *fiber.Ctx) error {
	var in models.ContactInput
	if err := c.BodyParser(&in); err != nil {
		return badRequest(c, "malformed request body")
	}
	if err := h.turnstile.Verify(c.Context(), turnstileToken(c), c.IP()); err != nil {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "bot_check_failed", "message": "Verification failed. Please retry.",
		})
	}
	if fieldErrs := h.validator.Struct(in); fieldErrs != nil {
		return validationFailed(c, fieldErrs)
	}
	if err := h.repo.CreateContact(c.Context(), in); err != nil {
		h.log.Error().Err(err).Msg("create contact failed")
		return internalError(c)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"ok": true, "message": "Message received."})
}

// ---- POST /v1/newsletter ---------------------------------------------------

func (h *Handler) Subscribe(c *fiber.Ctx) error {
	var in models.NewsletterInput
	if err := c.BodyParser(&in); err != nil {
		return badRequest(c, "malformed request body")
	}
	if err := h.turnstile.Verify(c.Context(), turnstileToken(c), c.IP()); err != nil {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "bot_check_failed", "message": "Verification failed. Please retry.",
		})
	}
	if fieldErrs := h.validator.Struct(in); fieldErrs != nil {
		return validationFailed(c, fieldErrs)
	}
	if err := h.repo.Subscribe(c.Context(), in); err != nil {
		h.log.Error().Err(err).Msg("subscribe failed")
		return internalError(c)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"ok": true, "message": "Subscribed."})
}

// ---- GET /v1/geo -----------------------------------------------------------

// Geo returns the authoritative 8-division / 64-district tree so the frontend's
// cascading select is driven by the SAME source the server validates against.
func (h *Handler) Geo(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"divisions": data.Divisions})
}

// ---- GET /v1/verify --------------------------------------------------------

// VerifyDocument confirms a verification code corresponds to a genuine issued
// document and reports its lifecycle status — WITHOUT returning any PII. It is
// what a QR scan on a printed admit card resolves to, so it must answer cleanly
// for the public: a valid code returns the status, an unknown code returns a
// 200 with valid:false (not an error), and only malformed input / server faults
// use non-2xx. No Turnstile: this is a read-only, non-PII authenticity check.
func (h *Handler) VerifyDocument(c *fiber.Ctx) error {
	code := strings.TrimSpace(c.Query("code"))
	if code == "" {
		return badRequest(c, "missing verification code")
	}
	// Bound the input so a pathological query can't reach the database layer.
	if len(code) > 64 {
		return c.JSON(fiber.Map{"ok": true, "valid": false})
	}

	view, err := h.repo.FindByVerifyCode(c.Context(), code)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return c.JSON(fiber.Map{"ok": true, "valid": false})
		}
		h.log.Error().Err(err).Msg("verify lookup failed")
		return internalError(c)
	}
	return c.JSON(fiber.Map{"ok": true, "valid": true, "result": view})
}

// ---- shared error helpers --------------------------------------------------

func badRequest(c *fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "bad_request", "message": msg})
}

func validationFailed(c *fiber.Ctx, fields map[string]string) error {
	return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{
		"error":  "validation_failed",
		"fields": fields,
	})
}

func internalError(c *fiber.Ctx) error {
	return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
		"error":   "internal_error",
		"message": "Something went wrong. Please try again.",
	})
}
