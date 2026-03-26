-- name: GetQuestion :one
SELECT id, category, question, picture, short, yes, no
FROM question WHERE id = ?;

-- name: GetRandomQuestion :one
SELECT id, category, question, picture, short, yes, no
FROM question ORDER BY RANDOM() LIMIT 1;

-- name: IncrementYes :exec
UPDATE question SET yes = yes + 1 WHERE id = ?;

-- name: IncrementNo :exec
UPDATE question SET no = no + 1 WHERE id = ?;

-- name: GetQuestionStats :one
SELECT short, yes, no FROM question WHERE id = ?;

-- name: InsertAnswer :exec
INSERT INTO answer (question, answer, submitter, submitteragent)
VALUES (?, ?, ?, ?);

-- name: CountAnswers :one
SELECT count(*) FROM answer;
