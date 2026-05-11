#!/bin/bash
export MONGODB_URI=mongodb://localhost:27017
export MONGODB_DATABASE=markdown_editor
exec /Users/ivanohotnikov/GoProjects/markdown-editor/backend/markdown-editor mcp 2>> /tmp/mcp-error.log
