package db

import (
	"context"
	"fmt"
)

// ListMaterials returns all course materials (optionally filtered to published only).
func (s *Store) ListMaterials(ctx context.Context, publishedOnly bool) ([]Material, error) {
	q := `SELECT id, module_number, lesson_number, title, COALESCE(description,''), COALESCE(content_markdown,''), COALESCE(video_url,''), duration_minutes, file_type, s3_master_key, is_published, created_at FROM materials`
	if publishedOnly {
		q += ` WHERE is_published = TRUE`
	}
	q += ` ORDER BY module_number ASC, lesson_number ASC, id ASC`

	rows, err := s.Pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []Material
	for rows.Next() {
		var m Material
		if err := rows.Scan(&m.ID, &m.ModuleNumber, &m.LessonNumber, &m.Title, &m.Description, &m.ContentMarkdown, &m.VideoURL, &m.DurationMinutes, &m.FileType, &m.S3MasterKey, &m.IsPublished, &m.CreatedAt); err != nil {
			return nil, err
		}
		res = append(res, m)
	}
	return res, rows.Err()
}

// GetMaterialByID fetches a single material.
func (s *Store) GetMaterialByID(ctx context.Context, id int) (*Material, error) {
	var m Material
	err := s.Pool.QueryRow(ctx, `
		SELECT id, module_number, lesson_number, title, COALESCE(description,''), COALESCE(content_markdown,''), COALESCE(video_url,''), duration_minutes, file_type, s3_master_key, is_published, created_at
		FROM materials WHERE id = $1`, id,
	).Scan(&m.ID, &m.ModuleNumber, &m.LessonNumber, &m.Title, &m.Description, &m.ContentMarkdown, &m.VideoURL, &m.DurationMinutes, &m.FileType, &m.S3MasterKey, &m.IsPublished, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// CreateMaterial inserts a new course material.
func (s *Store) CreateMaterial(ctx context.Context, m Material) (*Material, error) {
	var out Material
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO materials (module_number, lesson_number, title, description, content_markdown, video_url, duration_minutes, file_type, s3_master_key, is_published)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, module_number, lesson_number, title, COALESCE(description,''), COALESCE(content_markdown,''), COALESCE(video_url,''), duration_minutes, file_type, s3_master_key, is_published, created_at`,
		m.ModuleNumber, m.LessonNumber, m.Title, m.Description, m.ContentMarkdown, m.VideoURL, m.DurationMinutes, m.FileType, m.S3MasterKey, m.IsPublished,
	).Scan(&out.ID, &out.ModuleNumber, &out.LessonNumber, &out.Title, &out.Description, &out.ContentMarkdown, &out.VideoURL, &out.DurationMinutes, &out.FileType, &out.S3MasterKey, &out.IsPublished, &out.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateMaterial edits an existing material's metadata and optional master S3 key.
func (s *Store) UpdateMaterial(ctx context.Context, m Material) (*Material, error) {
	var out Material
	err := s.Pool.QueryRow(ctx, `
		UPDATE materials
		SET module_number = $2, lesson_number = $3, title = $4, description = $5, content_markdown = $6,
		    video_url = $7, duration_minutes = $8, file_type = $9,
		    s3_master_key = CASE WHEN $10 <> '' THEN $10 ELSE s3_master_key END,
		    is_published = $11, updated_at = NOW()
		WHERE id = $1
		RETURNING id, module_number, lesson_number, title, COALESCE(description,''), COALESCE(content_markdown,''), COALESCE(video_url,''), duration_minutes, file_type, s3_master_key, is_published, created_at`,
		m.ID, m.ModuleNumber, m.LessonNumber, m.Title, m.Description, m.ContentMarkdown, m.VideoURL, m.DurationMinutes, m.FileType, m.S3MasterKey, m.IsPublished,
	).Scan(&out.ID, &out.ModuleNumber, &out.LessonNumber, &out.Title, &out.Description, &out.ContentMarkdown, &out.VideoURL, &out.DurationMinutes, &out.FileType, &out.S3MasterKey, &out.IsPublished, &out.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ListCabinetMaterials returns published lessons enriched with user's watermark status and completion flag.
func (s *Store) ListCabinetMaterials(ctx context.Context, userID int64) ([]CabinetMaterial, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT m.id, m.module_number, m.lesson_number, m.title, COALESCE(m.description,''), COALESCE(m.content_markdown,''),
		       COALESCE(m.video_url,''), m.duration_minutes, m.file_type, m.s3_master_key, m.is_published, m.created_at,
		       COALESCE(cm.status, 'none') AS client_status,
		       cm.generated_at,
		       (ump.id IS NOT NULL) AS completed,
		       ump.completed_at
		FROM materials m
		LEFT JOIN client_materials cm ON cm.material_id = m.id AND cm.user_id = $1
		LEFT JOIN user_material_progress ump ON ump.material_id = m.id AND ump.user_id = $1
		WHERE m.is_published = TRUE
		ORDER BY m.module_number ASC, m.lesson_number ASC, m.id ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []CabinetMaterial
	for rows.Next() {
		var item CabinetMaterial
		if err := rows.Scan(
			&item.ID, &item.ModuleNumber, &item.LessonNumber, &item.Title, &item.Description, &item.ContentMarkdown,
			&item.VideoURL, &item.DurationMinutes, &item.FileType, &item.S3MasterKey, &item.IsPublished, &item.CreatedAt,
			&item.ClientStatus, &item.GeneratedAt, &item.Completed, &item.CompletedAt,
		); err != nil {
			return nil, err
		}
		res = append(res, item)
	}
	return res, rows.Err()
}

// SetMaterialCompleted records or clears lesson completion progress for a user.
func (s *Store) SetMaterialCompleted(ctx context.Context, userID int64, materialID int, completed bool) error {
	if completed {
		_, err := s.Pool.Exec(ctx, `
			INSERT INTO user_material_progress (user_id, material_id)
			VALUES ($1, $2) ON CONFLICT (user_id, material_id) DO NOTHING`, userID, materialID)
		return err
	}
	_, err := s.Pool.Exec(ctx, `DELETE FROM user_material_progress WHERE user_id = $1 AND material_id = $2`, userID, materialID)
	return err
}

// GetClientMaterial loads a user's personalized material record.
func (s *Store) GetClientMaterial(ctx context.Context, userID int64, materialID int) (*ClientMaterial, error) {
	var cm ClientMaterial
	err := s.Pool.QueryRow(ctx, `
		SELECT id, user_id, order_id, material_id, s3_client_key, watermark_signature, status, COALESCE(error_message,''), generated_at, created_at
		FROM client_materials WHERE user_id = $1 AND material_id = $2`, userID, materialID,
	).Scan(&cm.ID, &cm.UserID, &cm.OrderID, &cm.MaterialID, &cm.S3ClientKey, &cm.WatermarkSignature, &cm.Status, &cm.ErrorMessage, &cm.GeneratedAt, &cm.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &cm, nil
}

// ListClientMaterialsForUser returns all watermark jobs for a specific user (admin view).
func (s *Store) ListClientMaterialsForUser(ctx context.Context, userID int64) ([]ClientMaterial, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, user_id, order_id, material_id, s3_client_key, watermark_signature, status, COALESCE(error_message,''), generated_at, created_at
		FROM client_materials WHERE user_id = $1 ORDER BY material_id ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []ClientMaterial
	for rows.Next() {
		var cm ClientMaterial
		if err := rows.Scan(&cm.ID, &cm.UserID, &cm.OrderID, &cm.MaterialID, &cm.S3ClientKey, &cm.WatermarkSignature, &cm.Status, &cm.ErrorMessage, &cm.GeneratedAt, &cm.CreatedAt); err != nil {
			return nil, err
		}
		res = append(res, cm)
	}
	return res, rows.Err()
}

// EnsureClientMaterialForUser creates or resets a client_material record for re-watermarking.
func (s *Store) EnsureClientMaterialForUser(ctx context.Context, userID, orderID int64, orderNumber, fullName, email, phone string, materialID int) (*ClientMaterial, error) {
	key := fmt.Sprintf("clients/%d/materials/%d/document_%d.pdf", userID, materialID, orderID)
	sig := fmt.Sprintf("%s (%s, %s) • %s", fullName, email, phone, orderNumber)
	var cm ClientMaterial
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO client_materials (user_id, order_id, material_id, s3_client_key, watermark_signature, status)
		VALUES ($1, $2, $3, $4, $5, 'pending')
		ON CONFLICT (user_id, material_id) DO UPDATE SET
			order_id = EXCLUDED.order_id,
			s3_client_key = EXCLUDED.s3_client_key,
			watermark_signature = EXCLUDED.watermark_signature,
			status = 'pending',
			error_message = NULL,
			generated_at = NULL
		RETURNING id, user_id, order_id, material_id, s3_client_key, watermark_signature, status, COALESCE(error_message,''), generated_at, created_at`,
		userID, orderID, materialID, key, sig,
	).Scan(&cm.ID, &cm.UserID, &cm.OrderID, &cm.MaterialID, &cm.S3ClientKey, &cm.WatermarkSignature, &cm.Status, &cm.ErrorMessage, &cm.GeneratedAt, &cm.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &cm, nil
}
