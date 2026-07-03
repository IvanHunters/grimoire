.PHONY: up down backend frontend logs logs-backend logs-frontend

BACKEND_LOG  := /tmp/md-editor-backend.log
FRONTEND_LOG := /tmp/md-editor-frontend.log
LOCAL_IP     := $(shell ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null || hostname -I 2>/dev/null | awk '{print $$1}')

up: ## Start backend and frontend (accessible from local network)
	@$(MAKE) backend
	@$(MAKE) frontend

backend: ## Start backend on 0.0.0.0:8080 (kills existing)
	@lsof -ti :8080 | xargs kill -9 2>/dev/null || true
	@sleep 0.5
	@cd backend && ALLOWED_ORIGINS='*' USE_DAEMON_BACKEND=1 nohup go run cmd/markdown-editor/main.go serve > $(BACKEND_LOG) 2>&1 &
	@echo "Backend starting on :8080 (logs: $(BACKEND_LOG))"
	@sleep 3
	@curl --silent --max-time 2 http://localhost:8080/health > /dev/null && \
		echo "Backend ready — http://localhost:8080  |  http://$(LOCAL_IP):8080" || \
		echo "Backend not ready yet, check $(BACKEND_LOG)"

frontend: ## Start frontend dev server on 0.0.0.0:5173 (kills existing)
	@lsof -ti :5173 | xargs kill -9 2>/dev/null || true
	@sleep 0.5
	@cd frontend && nohup npm run dev > $(FRONTEND_LOG) 2>&1 &
	@echo "Frontend starting on :5173 (logs: $(FRONTEND_LOG))"
	@sleep 3
	@grep --quiet "ready" $(FRONTEND_LOG) && \
		echo "Frontend ready — http://localhost:5173  |  http://$(LOCAL_IP):5173" || \
		echo "Frontend not ready yet, check $(FRONTEND_LOG)"

down: ## Stop backend and frontend
	@lsof -ti :8080 | xargs kill -9 2>/dev/null || true
	@lsof -ti :5173 | xargs kill -9 2>/dev/null || true
	@echo "Stopped"

logs: ## Tail both logs
	@tail -f $(BACKEND_LOG) $(FRONTEND_LOG)

logs-backend: ## Tail backend log
	@tail -f $(BACKEND_LOG)

logs-frontend: ## Tail frontend log
	@tail -f $(FRONTEND_LOG)
