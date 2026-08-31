#!/bin/sh
# Minimal Snow RPC v1 fixture for desktop transport tests.

emit() {
    printf '%s\n' "$1"
}

printf '%s\n' "$$" > .mock-snow.pid
printf '%s\n' "$@" > .mock-snow-argv
if [ ! -f .mock-model ]; then
    printf '%s\n' 'mock-one' > .mock-model
fi
if [ ! -f .mock-thinking ]; then
    printf '%s\n' 'off' > .mock-thinking
fi
if [ ! -f .mock-branch ]; then
    printf '%s\n' 'main' > .mock-branch
fi
if [ ! -f .mock-session-name ]; then
    printf '%s\n' 'Desktop proof' > .mock-session-name
fi
emit '{"type":"rpc_ready","protocol_version":"1","snow_version":"mock-streaming","capabilities":["branch_management","messages_list","models_list","permission_interaction","prompt_completion","session_info","user_input"],"max_input_bytes":1048576}'

while IFS= read -r request; do
    request_id=$(printf '%s\n' "$request" | sed -n 's/^{"type":"[^"]*","id":"\([^"]*\)".*/\1/p')
    case "$request" in
        *'"type":"session_info"'*)
            model=$(cat .mock-model)
            thinking=$(cat .mock-thinking)
            if [ "$model" = 'mock-one' ]; then
                thinking_levels='["off","low","high"]'
            else
                thinking_levels='["off"]'
            fi
            session_name=$(cat .mock-session-name)
            emit "{\"type\":\"response\",\"id\":\"$request_id\",\"command\":\"session_info\",\"success\":true,\"data\":{\"session_id\":\"mock-session\",\"name\":\"$session_name\",\"path\":\"$PWD/mock-session.db\",\"cwd\":\"$PWD\",\"provider\":\"fake\",\"model\":\"$model\",\"thinking\":\"$thinking\",\"thinking_levels\":$thinking_levels}}"
            ;;
        *'"type":"messages_list"'*)
            if [ -f .mock-has-history ]; then
                messages='[{"id":"u1","role":"user","content":[{"type":"text","text":"restored question"}],"ts":1},{"id":"a1","parent_id":"u1","role":"assistant","content":[{"type":"text","text":"restored answer"}],"ts":2}]'
            else
                messages='[]'
            fi
            emit "{\"type\":\"response\",\"id\":\"$request_id\",\"command\":\"messages_list\",\"success\":true,\"data\":{\"messages\":$messages}}"
            ;;
        *'"type":"branches_list"'*)
            branch=$(cat .mock-branch)
            if [ ! -f .mock-has-fork ]; then
                branches='[{"id":"main","name":"Main","tip_id":"a1","messages":2,"created_at":1,"updated_at":2,"active":true}]'
            elif [ "$branch" = 'main' ]; then
                branches='[{"id":"main","name":"Main","tip_id":"a1","messages":2,"created_at":1,"updated_at":2,"active":true},{"id":"experiment","name":"Experiment","parent_branch_id":"main","forked_from_id":"a1","tip_id":"a1","messages":2,"created_at":3,"updated_at":3,"active":false}]'
            else
                branches='[{"id":"main","name":"Main","tip_id":"a1","messages":2,"created_at":1,"updated_at":2,"active":false},{"id":"experiment","name":"Experiment","parent_branch_id":"main","forked_from_id":"a1","tip_id":"a1","messages":2,"created_at":3,"updated_at":3,"active":true}]'
            fi
            emit "{\"type\":\"response\",\"id\":\"$request_id\",\"command\":\"branches_list\",\"success\":true,\"data\":{\"branches\":$branches}}"
            ;;
        *'"type":"branch_select"'*)
            branch=$(printf '%s\n' "$request" | sed -n 's/.*"branch_id":"\([^"]*\)".*/\1/p')
            printf '%s\n' "$branch" > .mock-branch
            emit "{\"type\":\"response\",\"id\":\"$request_id\",\"command\":\"branch_select\",\"success\":true}"
            emit '{"type":"session_updated"}'
            ;;
        *'"type":"branch_fork"'*)
            : > .mock-has-fork
            printf '%s\n' 'experiment' > .mock-branch
            emit "{\"type\":\"response\",\"id\":\"$request_id\",\"command\":\"branch_fork\",\"success\":true,\"data\":{\"id\":\"experiment\",\"name\":\"Experiment\",\"parent_branch_id\":\"main\",\"forked_from_id\":\"a1\",\"tip_id\":\"a1\",\"messages\":2,\"created_at\":3,\"updated_at\":3,\"active\":true}}"
            emit '{"type":"session_updated"}'
            ;;
        *'"type":"session_rename"'*)
            session_name=$(printf '%s\n' "$request" | sed -n 's/.*"name":"\([^"]*\)".*/\1/p')
            printf '%s\n' "$session_name" > .mock-session-name
            emit "{\"type\":\"response\",\"id\":\"$request_id\",\"command\":\"session_rename\",\"success\":true,\"data\":{\"session_id\":\"mock-session\",\"name\":\"$session_name\"}}"
            emit '{"type":"session_updated"}'
            ;;
        *'"type":"models_list"'*)
            model=$(cat .mock-model)
            emit "{\"type\":\"response\",\"id\":\"$request_id\",\"command\":\"models_list\",\"success\":true,\"data\":{\"provider\":\"fake\",\"current\":\"$model\",\"models\":[{\"provider\":\"fake\",\"id\":\"mock-one\",\"display_name\":\"Mock One\",\"supports_thinking\":true,\"default_thinking\":\"low\",\"thinking_levels\":[\"low\",\"high\"]},{\"provider\":\"fake\",\"id\":\"mock-two\",\"display_name\":\"Mock Two\"}]}}"
            ;;
        *'"type":"set_thinking"'*)
            thinking=$(printf '%s\n' "$request" | sed -n 's/.*"thinking":"\([^"]*\)".*/\1/p')
            printf '%s\n' "$thinking" > .mock-thinking
            emit "{\"type\":\"response\",\"id\":\"$request_id\",\"command\":\"set_thinking\",\"success\":true}"
            ;;
        *'"type":"set_model"'*)
            model=$(printf '%s\n' "$request" | sed -n 's/.*"model":"\([^"]*\)".*/\1/p')
            thinking=$(printf '%s\n' "$request" | sed -n 's/.*"thinking":"\([^"]*\)".*/\1/p')
            if [ "$model" = 'mock-two' ] && [ -n "$thinking" ] && [ "$thinking" != 'off' ]; then
                emit "{\"type\":\"response\",\"id\":\"$request_id\",\"command\":\"set_model\",\"success\":false,\"error\":\"unsupported thinking\"}"
                continue
            fi
            printf '%s\n' "$model" > .mock-model
            if [ -n "$thinking" ]; then
                printf '%s\n' "$thinking" > .mock-thinking
            fi
            emit "{\"type\":\"response\",\"id\":\"$request_id\",\"command\":\"set_model\",\"success\":true}"
            emit "{\"type\":\"model_changed\",\"model\":{\"provider\":\"fake\",\"id\":\"$model\"}}"
            ;;
        *'"type":"prompt"'*)
            emit "{\"type\":\"response\",\"id\":\"$request_id\",\"command\":\"prompt\",\"success\":true}"
            if printf '%s\n' "$request" | grep -q 'mismatched completion'; then
                emit '{"type":"permission_request","permission":{"request":{"id":"mismatch-perm","tool":"bash","args":{"command":"false"},"risk":"exec"}}}'
                emit '{"type":"prompt_completed","request_id":"wrong-prompt","status":"completed"}'
            elif printf '%s\n' "$request" | grep -q 'interactive milestone'; then
                printf '%s\n' "$request_id" > .mock-interaction-prompt
                printf '%s\n' 'permission' > .mock-interaction-phase
                emit '{"type":"permission_request","permission":{"request":{"id":"child-perm","tool":"bash","args":{"command":"false"},"risk":"exec"}},"agent":{"id":"child-1"}}'
                emit '{"type":"permission_request","permission":{"request":{"id":"perm-1","tool":"bash","args":{"command":"printf trusted"},"paths":["/tmp/mock-output"],"risk":"exec","reason":"integration proof"}}}'
            elif printf '%s\n' "$request" | grep -q 'malformed interaction'; then
                printf '%s\n' "$request_id" > .mock-interaction-prompt
                printf '%s\n' 'malformed' > .mock-interaction-phase
                emit '{"type":"permission_request","permission":{"request":{"id":"malformed-1","tool":"bash","args":{},"risk":17}}}'
            else
                emit '{"type":"text_delta","text":"streaming "}'
                sleep 0.05
                emit '{"type":"text_delta","text":"works"}'
                sleep 0.05
                emit '{"type":"turn_done","turn_id":"mock-turn"}'
                : > .mock-has-history
                emit '{"type":"session_updated"}'
                emit "{\"type\":\"prompt_completed\",\"request_id\":\"$request_id\",\"status\":\"completed\"}"
            fi
            ;;
        *'"type":"permission_reply"'*)
            phase=$(cat .mock-interaction-phase 2>/dev/null)
            if [ "$phase" != 'permission' ] || ! printf '%s\n' "$request" | grep -q '"request_id":"perm-1","decision":"allow"'; then
                emit "{\"type\":\"response\",\"id\":\"$request_id\",\"command\":\"permission_reply\",\"success\":false,\"error\":\"unexpected permission reply\"}"
                continue
            fi
            emit "{\"type\":\"response\",\"id\":\"$request_id\",\"command\":\"permission_reply\",\"success\":true}"
            printf '%s\n' 'user-input' > .mock-interaction-phase
            emit '{"type":"user_input_request","user_input":{"id":"ask-1","tool_call_id":"ask-1","questions":[{"id":"language","header":"Language","question":"Which language?","options":[{"label":"Rust","description":"Safe systems language"},{"label":"Go","description":"Simple concurrency"}]},{"id":"reason","header":"Reason","question":"Why?"}]}}'
            ;;
        *'"type":"permission_reject"'*)
            phase=$(cat .mock-interaction-phase 2>/dev/null)
            if [ "$phase" != 'malformed' ] || ! printf '%s\n' "$request" | grep -q '"request_id":"malformed-1"'; then
                emit "{\"type\":\"response\",\"id\":\"$request_id\",\"command\":\"permission_reject\",\"success\":false,\"error\":\"unexpected permission rejection\"}"
                continue
            fi
            emit "{\"type\":\"response\",\"id\":\"$request_id\",\"command\":\"permission_reject\",\"success\":true}"
            prompt_id=$(cat .mock-interaction-prompt)
            emit "{\"type\":\"prompt_completed\",\"request_id\":\"$prompt_id\",\"status\":\"failed\",\"error\":\"malformed interaction rejected\"}"
            rm -f .mock-interaction-phase .mock-interaction-prompt
            ;;
        *'"type":"user_input_reply"'*)
            phase=$(cat .mock-interaction-phase 2>/dev/null)
            if [ "$phase" != 'user-input' ] || ! printf '%s\n' "$request" | grep -q '"request_id":"ask-1","answers":\[{"id":"language","answer":"Rust"},{"id":"reason","answer":"Safety"}\]'; then
                emit "{\"type\":\"response\",\"id\":\"$request_id\",\"command\":\"user_input_reply\",\"success\":false,\"error\":\"unexpected user input reply\"}"
                continue
            fi
            emit "{\"type\":\"response\",\"id\":\"$request_id\",\"command\":\"user_input_reply\",\"success\":true}"
            prompt_id=$(cat .mock-interaction-prompt)
            emit '{"type":"text_delta","text":"continued after trusted replies"}'
            emit '{"type":"turn_done","turn_id":"mock-interaction-turn"}'
            emit "{\"type\":\"prompt_completed\",\"request_id\":\"$prompt_id\",\"status\":\"completed\"}"
            rm -f .mock-interaction-phase .mock-interaction-prompt
            ;;
        *'"type":"user_input_reject"'*)
            emit "{\"type\":\"response\",\"id\":\"$request_id\",\"command\":\"user_input_reject\",\"success\":false,\"error\":\"fixture rejection\"}"
            ;;
        *'"type":"abort"'*)
            emit "{\"type\":\"response\",\"id\":\"$request_id\",\"command\":\"abort\",\"success\":true}"
            ;;
    esac
done
