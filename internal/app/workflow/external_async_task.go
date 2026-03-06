package workflow

import (
	"strconv"
	"strings"
	"time"

	"flowweave/internal/domain/workflow/port"
)

const asyncTaskMetadataKey = "external_async_task"

// BuildExternalAsyncTasksFromNodeExecs extracts external async task records from node metadata.
func BuildExternalAsyncTasksFromNodeExecs(run *port.WorkflowRun, nodeExecs []port.NodeExecution) []*port.ExternalAsyncTask {
	if run == nil || len(nodeExecs) == 0 {
		return nil
	}
	result := make([]*port.ExternalAsyncTask, 0, len(nodeExecs))
	for _, exec := range nodeExecs {
		metaMap, ok := exec.Metadata[asyncTaskMetadataKey].(map[string]interface{})
		if !ok || metaMap == nil {
			continue
		}

		provider := asString(metaMap["provider"])
		providerRef := asString(metaMap["provider_task_ref"])
		if provider == "" || providerRef == "" {
			continue
		}

		task := &port.ExternalAsyncTask{
			TaskType:         nonEmpty(asString(metaMap["task_type"]), "asr"),
			Provider:         provider,
			ProviderTaskRef:  providerRef,
			RunID:            run.ID,
			WorkflowID:       run.WorkflowID,
			NodeID:           nonEmpty(exec.NodeID, asString(metaMap["node_id"])),
			OrgID:            run.OrgID,
			TenantID:         run.TenantID,
			ConversationID:   nonEmpty(run.ConversationID, asString(metaMap["conversation_id"])),
			Status:           normalizeAsyncTaskStatus(asString(metaMap["status"])),
			PollCount:        asInt(metaMap["poll_count"]),
			CallbackMode:     asString(metaMap["callback_mode"]),
			CallbackToken:    asString(metaMap["callback_token"]),
			CallbackURL:      asString(metaMap["callback_url"]),
			SubmitPayload:    asMap(metaMap["submit_payload"]),
			QueryPayload:     asMap(metaMap["query_payload"]),
			ResultPayload:    asMap(metaMap["result_payload"]),
			NormalizedResult: asMap(metaMap["normalized_result"]),
			ResultURL:        asString(metaMap["result_url"]),
			ErrorCode:        asString(metaMap["error_code"]),
			ErrorMessage:     asString(metaMap["error_message"]),
		}
		if task.Status == "" {
			task.Status = port.AsyncTaskStatusSubmitted
		}

		task.SubmittedAt = nonZeroTime(
			asTime(metaMap["submitted_at"]),
			exec.StartedAt,
			time.Now(),
		)
		task.StartedAt = asTimePtr(metaMap["started_at"])
		task.CompletedAt = asTimePtr(metaMap["completed_at"])
		task.CallbackReceivedAt = asTimePtr(metaMap["callback_received_at"])
		task.NextPollAt = asTimePtr(metaMap["next_poll_at"])

		result = append(result, task)
	}
	return result
}

func normalizeAsyncTaskStatus(v string) port.AsyncTaskStatus {
	s := strings.ToLower(strings.TrimSpace(v))
	switch s {
	case "submitted", "queued", "waiting", "":
		return port.AsyncTaskStatusSubmitted
	case "running", "doing", "processing", "in_progress":
		return port.AsyncTaskStatusRunning
	case "succeeded", "success", "completed":
		return port.AsyncTaskStatusSucceeded
	case "failed", "error":
		return port.AsyncTaskStatusFailed
	case "timeout", "timed_out":
		return port.AsyncTaskStatusTimeout
	case "canceled", "cancelled":
		return port.AsyncTaskStatusCanceled
	case "expired":
		return port.AsyncTaskStatusExpired
	default:
		return port.AsyncTaskStatusRunning
	}
}

func asString(v interface{}) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func asInt(v interface{}) int {
	switch x := v.(type) {
	case int:
		return x
	case int32:
		return int(x)
	case int64:
		return int(x)
	case float64:
		return int(x)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(x))
		return i
	default:
		return 0
	}
}

func asMap(v interface{}) map[string]interface{} {
	m, _ := v.(map[string]interface{})
	return m
}

func asTime(v interface{}) time.Time {
	switch x := v.(type) {
	case time.Time:
		return x
	case *time.Time:
		if x != nil {
			return *x
		}
	case string:
		t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(x))
		if err == nil {
			return t
		}
		t, err = time.Parse(time.RFC3339, strings.TrimSpace(x))
		if err == nil {
			return t
		}
	}
	return time.Time{}
}

func asTimePtr(v interface{}) *time.Time {
	t := asTime(v)
	if t.IsZero() {
		return nil
	}
	return &t
}

func nonZeroTime(times ...time.Time) time.Time {
	for _, t := range times {
		if !t.IsZero() {
			return t
		}
	}
	return time.Now()
}

func nonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
