package postgres

import (
	"database/sql"
	"errors"
	_ "github.com/lib/pq"
	"graphql-comments/storage"
	"graphql-comments/types"
	"time"
)

type DataStorePostgres struct {
	DB *sql.DB
}

func NewPostgresDataStore(dbURL string) (*DataStorePostgres, error) {
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		return nil, err
	}

	err = db.Ping()
	if err != nil {
		return nil, err
	}

	return &DataStorePostgres{DB: db}, nil
}

func (store *DataStorePostgres) AddPost(title, content string, allowComments bool) (*types.Post, error) {
	post := &types.Post{
		ID:            storage.GenerateNewPostUUID(),
		Title:         title,
		Content:       content,
		CreatedAt:     time.Now(),
		Comments:      []string{},
		AllowComments: allowComments,
	}

	_, err := store.DB.Exec("INSERT INTO posts (id, title, content, created_at) VALUES ($1, $2, $3, $4)",
		post.ID, post.Title, post.Content, post.CreatedAt)
	if err != nil {
		return nil, err
	}

	return post, nil
}

func (store *DataStorePostgres) AddComment(postID, parentCommentID, content string) (*types.Comment, error) {
	post, err := store.GetPostByID(postID)
	if err != nil {
		return nil, err
	}
	if !post.AllowComments {
		return nil, errors.New("comments are not allowed for this post")
	}
	comment := &types.Comment{
		ID:              storage.GenerateNewCommentUUID(),
		PostID:          postID,
		ParentCommentID: parentCommentID,
		Content:         content,
		CreatedAt:       time.Now(),
	}

	if comment.ParentCommentID == "" {
		if _, err := store.DB.Exec("INSERT INTO comments (id, post_id, parent_comment_id, content, created_at) VALUES ($1, $2, NULL, $3, $4)",
			comment.ID, comment.PostID, comment.Content, comment.CreatedAt,
		); err != nil {
			return nil, err
		}
	} else {
		if _, err := store.DB.Exec("INSERT INTO comments (id, post_id, parent_comment_id, content, created_at) VALUES ($1, $2, $3, $4, $5)",
			comment.ID, comment.PostID, comment.ParentCommentID, comment.Content, comment.CreatedAt,
		); err != nil {
			return nil, err
		}
	}

	return comment, nil
}

func (store *DataStorePostgres) GetPosts() ([]*types.Post, error) {
	rows, err := store.DB.Query("SELECT id, title, content, created_at, allow_comments FROM posts")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	posts := make([]*types.Post, 0)
	for rows.Next() {
		post := &types.Post{}
		err := rows.Scan(&post.ID, &post.Title, &post.Content, &post.CreatedAt, &post.AllowComments)
		if err != nil {
			return nil, err
		}

		commentRows, err := store.DB.Query("SELECT id FROM comments WHERE post_id = $1", post.ID)
		if err != nil {
			return nil, err
		}

		for commentRows.Next() {
			var commentID string
			if err := commentRows.Scan(&commentID); err != nil {
				return nil, err
			}
			post.Comments = append(post.Comments, commentID)
		}

		posts = append(posts, post)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return posts, nil
}

func (store *DataStorePostgres) GetPostByID(id string) (*types.Post, error) {
	post := &types.Post{}
	err := store.DB.QueryRow("SELECT id, title, content, created_at, allow_comments FROM posts WHERE id = $1", id).Scan(
		&post.ID,
		&post.Title,
		&post.Content,
		&post.CreatedAt,
		&post.AllowComments,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("post not found")
		}
		return nil, err
	}

	commentRows, err := store.DB.Query("SELECT id FROM comments WHERE post_id = $1", post.ID)
	if err != nil {
		return nil, err
	}

	for commentRows.Next() {
		var commentID string
		if err := commentRows.Scan(&commentID); err != nil {
			return nil, err
		}
		post.Comments = append(post.Comments, commentID)
	}

	return post, nil
}

func (store *DataStorePostgres) GetComments(postID string, page int) ([]*types.Comment, error) {
	rows, err := store.DB.Query("SELECT id, post_id, parent_comment_id, content, created_at FROM comments WHERE post_id = $1", postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	comments := make([]*types.Comment, 0)
	cnt := 0
	for rows.Next() && cnt < storage.CommentsPageSize*(page-1) {
		cnt++
		if err := rows.Scan(); err != nil {
			return nil, err
		}
	}

	for rows.Next() && cnt < storage.CommentsPageSize*page {
		comment := &types.Comment{}
		err := rows.Scan(&comment.ID, &comment.PostID, &comment.ParentCommentID, &comment.Content, &comment.CreatedAt)
		if err != nil {
			return nil, err
		}
		comments = append(comments, comment)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return comments, nil
}

func (store *DataStorePostgres) GetCommentByID(id string) (*types.Comment, error) {
	comment := &types.Comment{}
	var tmp sql.NullString
	err := store.DB.QueryRow(
		"SELECT id, post_id, parent_comment_id, content, created_at FROM comments WHERE id = $1", id).Scan(
		&comment.ID,
		&comment.PostID,
		&tmp,
		&comment.Content,
		&comment.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("comment not found")
		}
		return nil, err
	}

	if tmp.Valid {
		comment.ParentCommentID = tmp.String
	}

	repliesRows, err := store.DB.Query("SELECT id FROM comments WHERE parent_comment_id = $1", comment.ID)
	if err != nil {
		return nil, err
	}

	for repliesRows.Next() {
		var replyID string
		if err := repliesRows.Scan(&replyID); err != nil {
			return nil, err
		}
		comment.Replies = append(comment.Replies, replyID)
	}

	return comment, nil
}

func (store *DataStorePostgres) GetNumberOfCommentPages(postID string) (int, error) {
	var count int
	err := store.DB.QueryRow("SELECT COUNT(*) FROM comments WHERE post_id = $1", postID).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count/storage.CommentsPageSize + 1, nil
}

func (store *DataStorePostgres) GetReplies(commentID string) ([]*types.Comment, error) {
	rows, err := store.DB.Query("SELECT id FROM comments WHERE parent_comment_id = $1", commentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	comments := make([]*types.Comment, 0)
	for rows.Next() {
		var replyID string
		if err := rows.Scan(&replyID); err != nil {
			return nil, err
		}

		comment, err := store.GetCommentByID(commentID)
		if err != nil {
			return nil, err
		}
		comments = append(comments, comment)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return comments, nil
}
