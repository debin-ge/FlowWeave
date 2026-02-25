package memory

import "errors"

var (
	// ErrMidTermRequiresShortTerm 中期记忆需要短期记忆
	ErrMidTermRequiresShortTerm = errors.New("mid_term memory requires short_term memory to be enabled")

	// ErrLongTermRequiresMidTerm 长期记忆需要中期和短期记忆
	ErrLongTermRequiresMidTerm = errors.New("long_term memory requires mid_term memory to be enabled")

	// ErrConversationIDRequired 需要 conversation_id
	ErrConversationIDRequired = errors.New("conversation_id is required when memory is enabled")

	// ErrSTMVersionConflict STM 乐观锁版本冲突
	ErrSTMVersionConflict = errors.New("stm version conflict")
)
