package database

// projectSessionMessage composes the runtime message owned by entry from its
// ordered canonical parts. The returned parts do not alias the input.
func projectSessionMessage(entry *EntryEntity, parts []MessagePartEntity) SessionMessageEntity {
	createdAt := entry.Message.Timestamp
	if createdAt.IsZero() {
		createdAt = entry.CreatedAt
	}

	projectedParts := cloneMessageParts(parts)

	sender := string(entry.Message.Role)
	if entry.Message.Role == RoleCustom && entry.CustomType != "" {
		sender = entry.CustomType
	}

	return SessionMessageEntity{
		CreatedAt: createdAt,
		SessionID: entry.SessionID,
		EntryID:   entry.ID,
		Sender:    sender,
		Role:      entry.Message.Role,
		Content:   messagePartsText(projectedParts),
		Provider:  entry.Message.Provider,
		Model:     entry.Message.Model,
		Parts:     projectedParts,
	}
}
