-- Graph authoring moved to the chat-session mechanism the agent-creation studio
-- already uses (see handler/graph_builder.go): one hidden carrier agent and one
-- chat_session per creation, with the draft travelling inside the assistant's
-- replies. Nothing server-side needs to remember a submission count, a request
-- payload or an alias map, because there is no longer a single-shot submission
-- to budget, replay or reject.
--
-- Migrations 377 and 378 remove the indexes first, so their down migrations
-- restore them individually after this migration restores the table.
DROP TABLE IF EXISTS graph_generation;
