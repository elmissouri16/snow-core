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
emit '{"type":"rpc_ready","protocol_version":"1","snow_version":"mock-streaming","capabilities":["branch_management","messages_list","models_list","prompt_completion","session_info"],"max_input_bytes":1048576}'

while IFS= read -r request; do
    request_id=$(printf '%s\n' "$request" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
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
            emit '{"type":"text_delta","text":"streaming "}'
            sleep 0.05
            emit '{"type":"text_delta","text":"works"}'
            sleep 0.05
            emit '{"type":"turn_done","turn_id":"mock-turn"}'
            : > .mock-has-history
            emit '{"type":"session_updated"}'
            emit "{\"type\":\"prompt_completed\",\"request_id\":\"$request_id\",\"status\":\"completed\"}"
            ;;
        *'"type":"abort"'*)
            emit "{\"type\":\"response\",\"id\":\"$request_id\",\"command\":\"abort\",\"success\":true}"
            ;;
    esac
done
