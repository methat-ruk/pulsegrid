package rules

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/telemetry/ingestion"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("rules repository requires a database pool")
	}
	return &Repository{pool: pool}, nil
}

func (r *Repository) ValidateSchema(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var available bool
	err := r.pool.QueryRow(ctx, `
		SELECT NOT EXISTS (
			SELECT 1
			FROM (VALUES
				('threshold_rules'::text, 'id'::text),
				('threshold_rules'::text, 'device_id'::text),
				('threshold_rules'::text, 'metric'::text),
				('threshold_rules'::text, 'comparator'::text),
				('threshold_rules'::text, 'threshold_celsius'::text),
				('threshold_rules'::text, 'enabled'::text),
				('threshold_rules'::text, 'revision'::text),
				('threshold_rules'::text, 'created_at'::text),
				('threshold_rules'::text, 'updated_at'::text),
				('threshold_alerts'::text, 'id'::text),
				('threshold_alerts'::text, 'rule_id'::text),
				('threshold_alerts'::text, 'device_id'::text),
				('threshold_alerts'::text, 'message_id'::text),
				('threshold_alerts'::text, 'observed_at'::text),
				('threshold_alerts'::text, 'received_at'::text),
				('threshold_alerts'::text, 'temperature_celsius'::text),
				('threshold_alerts'::text, 'metric'::text),
				('threshold_alerts'::text, 'comparator'::text),
				('threshold_alerts'::text, 'threshold_celsius'::text),
				('threshold_alerts'::text, 'created_at'::text)
			) AS required(table_name, column_name)
			WHERE NOT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = current_schema()
				  AND table_name = required.table_name
				  AND column_name = required.column_name
			)
		)
	`).Scan(&available)
	if err != nil {
		return fmt.Errorf("validate threshold rules schema: %w", err)
	}
	if !available {
		return ErrSchemaUnavailable
	}
	return nil
}

var ErrSchemaUnavailable = errors.New("threshold rules schema is unavailable")

func (r *Repository) CreateRule(ctx context.Context, organizationID uuid.UUID, input CreateRuleInput) (Rule, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if organizationID == uuid.Nil || input.DeviceID == uuid.Nil || validateRuleInput(input.Comparator, input.ThresholdCelsius) != nil {
		return Rule{}, ErrInvalidInput
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Rule{}, fmt.Errorf("begin threshold rule creation: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var deviceID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT id FROM devices
		WHERE organization_id = $1 AND id = $2
		FOR UPDATE
	`, organizationID, input.DeviceID).Scan(&deviceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Rule{}, ErrNotFound
	}
	if err != nil {
		return Rule{}, fmt.Errorf("lock device for threshold rule creation: %w", err)
	}
	var ruleCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM threshold_rules WHERE device_id = $1`, deviceID).Scan(&ruleCount); err != nil {
		return Rule{}, fmt.Errorf("count device threshold rules: %w", err)
	}
	if ruleCount >= MaxRulesPerDevice {
		return Rule{}, ErrRuleLimitReached
	}
	created, err := scanRule(tx.QueryRow(ctx, `
		INSERT INTO threshold_rules (id, device_id, comparator, threshold_celsius, enabled)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, device_id, metric, comparator, threshold_celsius, enabled, revision, created_at, updated_at
	`, uuid.New(), deviceID, input.Comparator, input.ThresholdCelsius, input.Enabled))
	if err != nil {
		return Rule{}, fmt.Errorf("insert threshold rule: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Rule{}, fmt.Errorf("commit threshold rule creation: %w", err)
	}
	return created, nil
}

func (r *Repository) UpdateRule(ctx context.Context, organizationID uuid.UUID, input UpdateRuleInput) (Rule, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if organizationID == uuid.Nil || input.ID == uuid.Nil || input.ExpectedRevision < 1 || validateRuleInput(input.Comparator, input.ThresholdCelsius) != nil {
		return Rule{}, ErrInvalidInput
	}
	updated, err := scanRule(r.pool.QueryRow(ctx, `
		UPDATE threshold_rules AS r
		SET comparator = $3,
		    threshold_celsius = $4,
		    enabled = $5,
		    revision = revision + 1,
		    updated_at = clock_timestamp()
		WHERE r.id = $1
	  AND r.revision = $2
	  AND EXISTS (
		SELECT 1 FROM devices AS d
		WHERE d.id = r.device_id AND d.organization_id = $6
	  )
		RETURNING r.id, r.device_id, r.metric, r.comparator, r.threshold_celsius, r.enabled, r.revision, r.created_at, r.updated_at
	`, input.ID, input.ExpectedRevision, input.Comparator, input.ThresholdCelsius, input.Enabled, organizationID))
	if err == nil {
		return updated, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Rule{}, fmt.Errorf("update threshold rule: %w", err)
	}
	var currentRevision int
	lookupErr := r.pool.QueryRow(ctx, `
		SELECT r.revision
		FROM threshold_rules AS r
		JOIN devices AS d ON d.id = r.device_id
		WHERE r.id = $1 AND d.organization_id = $2
	`, input.ID, organizationID).Scan(&currentRevision)
	if errors.Is(lookupErr, pgx.ErrNoRows) {
		return Rule{}, ErrNotFound
	}
	if lookupErr != nil {
		return Rule{}, fmt.Errorf("check threshold rule revision: %w", lookupErr)
	}
	return Rule{}, ErrConflict
}

func (r *Repository) ListRules(ctx context.Context, organizationID, deviceID uuid.UUID) ([]Rule, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if organizationID == uuid.Nil || deviceID == uuid.Nil {
		return nil, ErrInvalidInput
	}
	rows, err := r.pool.Query(ctx, `
		SELECT r.id, r.device_id, r.metric, r.comparator, r.threshold_celsius, r.enabled, r.revision, r.created_at, r.updated_at
		FROM threshold_rules AS r
		JOIN devices AS d ON d.id = r.device_id
		WHERE d.organization_id = $1 AND r.device_id = $2
		ORDER BY r.id ASC
		LIMIT $3
	`, organizationID, deviceID, MaxRulesPerDevice)
	if err != nil {
		return nil, fmt.Errorf("list device threshold rules: %w", err)
	}
	defer rows.Close()
	rules := make([]Rule, 0)
	for rows.Next() {
		var rule Rule
		if err := rows.Scan(&rule.ID, &rule.DeviceID, &rule.Metric, &rule.Comparator, &rule.ThresholdCelsius, &rule.Enabled, &rule.Revision, &rule.CreatedAt, &rule.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan threshold rule: %w", err)
		}
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate threshold rules: %w", err)
	}
	return rules, nil
}

func (r *Repository) GetAlert(ctx context.Context, organizationID, alertID uuid.UUID) (Alert, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if organizationID == uuid.Nil || alertID == uuid.Nil {
		return Alert{}, ErrInvalidInput
	}
	alert, err := scanAlert(r.pool.QueryRow(ctx, alertSelect+`
		WHERE d.organization_id = $1 AND a.id = $2
	`, organizationID, alertID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Alert{}, ErrNotFound
	}
	if err != nil {
		return Alert{}, fmt.Errorf("read threshold alert: %w", err)
	}
	return alert, nil
}

func (r *Repository) ListAlerts(ctx context.Context, organizationID uuid.UUID, pageSize int, deviceFilter *uuid.UUID, cursor *AlertCursor) (AlertPage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if organizationID == uuid.Nil || pageSize < 1 || pageSize > MaxAlertPageSize || (deviceFilter != nil && *deviceFilter == uuid.Nil) {
		return AlertPage{}, ErrInvalidInput
	}
	if cursor != nil && (cursor.CreatedAt.IsZero() || cursor.ID == uuid.Nil || cursor.OrganizationID != organizationID || !sameDeviceFilter(cursor.DeviceFilter, deviceFilter)) {
		return AlertPage{}, ErrInvalidInput
	}
	var cursorTime any
	var cursorID any
	var scopedDevice any
	if cursor != nil {
		cursorTime = cursor.CreatedAt
		cursorID = cursor.ID
	}
	if deviceFilter != nil {
		scopedDevice = *deviceFilter
	}
	rows, err := r.pool.Query(ctx, alertSelect+`
		WHERE d.organization_id = $1
		  AND ($2::uuid IS NULL OR a.device_id = $2)
		  AND ($3::timestamptz IS NULL OR (a.created_at, a.id) < ($3, $4))
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT $5
	`, organizationID, scopedDevice, cursorTime, cursorID, pageSize+1)
	if err != nil {
		return AlertPage{}, fmt.Errorf("list threshold alerts: %w", err)
	}
	defer rows.Close()
	alerts := make([]Alert, 0, pageSize+1)
	for rows.Next() {
		alert, scanErr := scanAlertRow(rows)
		if scanErr != nil {
			return AlertPage{}, fmt.Errorf("scan threshold alert: %w", scanErr)
		}
		alerts = append(alerts, alert)
	}
	if err := rows.Err(); err != nil {
		return AlertPage{}, fmt.Errorf("iterate threshold alerts: %w", err)
	}
	page := AlertPage{Alerts: alerts}
	if len(alerts) > pageSize {
		page.Alerts = alerts[:pageSize]
		last := page.Alerts[len(page.Alerts)-1]
		page.NextCursor = &AlertCursor{
			CreatedAt:      last.CreatedAt,
			ID:             last.ID,
			OrganizationID: organizationID,
			DeviceFilter:   cloneUUID(deviceFilter),
		}
	}
	return page, nil
}

// Evaluate applies the enabled rules visible in tx and inserts matching alert
// snapshots. The caller owns tx and commits or rolls back telemetry and alerts
// together.
func (r *Repository) Evaluate(ctx context.Context, tx pgx.Tx, accepted ingestion.AcceptedTelemetry) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if tx == nil || accepted.OrganizationID == uuid.Nil || accepted.DeviceID == uuid.Nil || accepted.MessageID == uuid.Nil || math.IsNaN(accepted.TemperatureCelsius) || math.IsInf(accepted.TemperatureCelsius, 0) {
		return ErrEvaluationFailed
	}
	rows, err := tx.Query(ctx, `
		SELECT id, comparator, threshold_celsius
		FROM threshold_rules
		WHERE device_id = $1 AND enabled = true
		ORDER BY id ASC
	`, accepted.DeviceID)
	if err != nil {
		return fmt.Errorf("%w: read enabled rules", ErrEvaluationFailed)
	}
	type evaluationRule struct {
		id         uuid.UUID
		comparator Comparator
		threshold  float64
	}
	enabledRules := make([]evaluationRule, 0, MaxRulesPerDevice)
	for rows.Next() {
		var rule evaluationRule
		if err := rows.Scan(&rule.id, &rule.comparator, &rule.threshold); err != nil {
			rows.Close()
			return fmt.Errorf("%w: scan enabled rules", ErrEvaluationFailed)
		}
		enabledRules = append(enabledRules, rule)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("%w: iterate enabled rules", ErrEvaluationFailed)
	}
	rows.Close()

	for _, rule := range enabledRules {
		matched, err := Compare(rule.comparator, accepted.TemperatureCelsius, rule.threshold)
		if err != nil {
			return fmt.Errorf("%w: stored rule is invalid", ErrEvaluationFailed)
		}
		if !matched {
			continue
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO threshold_alerts (
				id, rule_id, device_id, message_id, observed_at, received_at,
				temperature_celsius, metric, comparator, threshold_celsius
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`, uuid.New(), rule.id, accepted.DeviceID, accepted.MessageID,
			accepted.ObservedAt.UTC(), accepted.ReceivedAt.UTC(), accepted.TemperatureCelsius,
			MetricTemperatureCelsius, rule.comparator, rule.threshold)
		if err != nil {
			return fmt.Errorf("%w: insert alert occurrence", ErrAlertPersistence)
		}
	}
	return nil
}

const alertSelect = `
	SELECT a.id, a.rule_id, a.device_id, a.message_id, a.observed_at,
	       a.received_at, a.temperature_celsius, a.metric, a.comparator,
	       a.threshold_celsius, a.created_at
	FROM threshold_alerts AS a
	JOIN devices AS d ON d.id = a.device_id
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRule(row rowScanner) (Rule, error) {
	var rule Rule
	err := row.Scan(&rule.ID, &rule.DeviceID, &rule.Metric, &rule.Comparator, &rule.ThresholdCelsius, &rule.Enabled, &rule.Revision, &rule.CreatedAt, &rule.UpdatedAt)
	return rule, err
}

func scanAlert(row rowScanner) (Alert, error) {
	var alert Alert
	err := row.Scan(&alert.ID, &alert.RuleID, &alert.DeviceID, &alert.MessageID, &alert.ObservedAt, &alert.ReceivedAt, &alert.TemperatureCelsius, &alert.Metric, &alert.Comparator, &alert.ThresholdCelsius, &alert.CreatedAt)
	return alert, err
}

func scanAlertRow(rows pgx.Rows) (Alert, error) { return scanAlert(rows) }

func sameDeviceFilter(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func cloneUUID(value *uuid.UUID) *uuid.UUID {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
