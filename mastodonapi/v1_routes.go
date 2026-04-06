package mastodonapi

import "net/http"

// mountMastodon registers the Mastodon 4.x–compatible REST surface Mastodon clients expect
// (both /api/v1/* and the /api/v2/* entrypoints current apps prefer for instance, search, etc.).
// Handlers are stubs or minimal implementations unless documented otherwise in package README.
func (s *Server) mountMastodon(mux *http.ServeMux) {
	b := s.bearer

	mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.getOAuthAuthorizationServer)
	mux.HandleFunc("POST /api/v1/apps", s.postApps)
	mux.HandleFunc("GET /oauth/authorize", s.getOAuthAuthorize)
	mux.HandleFunc("POST /oauth/authorize", s.postOAuthAuthorize)
	mux.HandleFunc("POST /oauth/token", s.postOAuthToken)
	mux.HandleFunc("POST /oauth/revoke", s.postOAuthRevoke)

	// Instance
	mux.HandleFunc("GET /api/v1/instance", s.getInstance)
	mux.HandleFunc("GET /api/v2/instance", s.getInstanceV2)
	mux.HandleFunc("GET /api/v2/search", s.getSearchV1)
	mux.HandleFunc("GET /api/v2/filters", b(s.getFiltersV2))
	mux.HandleFunc("GET /api/v2/suggestions", s.getSuggestionsV1)
	mux.HandleFunc("GET /api/v1/instance/extended_description", s.getInstanceExtendedDescription)
	mux.HandleFunc("GET /api/v1/instance/peers", s.getInstancePeers)
	mux.HandleFunc("GET /api/v1/instance/activity", s.getInstanceActivity)
	mux.HandleFunc("GET /api/v1/instance/rules", s.getInstanceRules)
	mux.HandleFunc("GET /api/v1/instance/translation_languages", s.getInstanceTranslationLanguages)
	mux.HandleFunc("GET /api/v1/instance/domain_blocks", s.getInstanceDomainBlocks)

	// Accounts (literals before /{id})
	mux.HandleFunc("GET /api/v1/accounts/verify_credentials", b(s.getVerifyCredentials))
	mux.HandleFunc("PATCH /api/v1/accounts/update_credentials", b(s.patchAccountUpdateCredentials))
	mux.HandleFunc("GET /api/v1/accounts/search", s.getAccountSearch)
	mux.HandleFunc("GET /api/v1/accounts/lookup", s.getAccountLookup)
	mux.HandleFunc("GET /api/v1/accounts/familiar_followers", b(s.getFamiliarFollowers))
	mux.HandleFunc("GET /api/v1/accounts/relationships", b(s.getAccountRelationships))

	mux.HandleFunc("POST /api/v1/accounts/{id}/follow", b(s.postAccountFollow))
	mux.HandleFunc("POST /api/v1/accounts/{id}/unfollow", b(s.postAccountUnfollow))
	mux.HandleFunc("POST /api/v1/accounts/{id}/block", b(s.v1StubBearerEmptyObject))
	mux.HandleFunc("POST /api/v1/accounts/{id}/unblock", b(s.v1StubBearerEmptyObject))
	mux.HandleFunc("POST /api/v1/accounts/{id}/mute", b(s.v1StubBearerEmptyObject))
	mux.HandleFunc("POST /api/v1/accounts/{id}/unmute", b(s.v1StubBearerEmptyObject))
	mux.HandleFunc("POST /api/v1/accounts/{id}/pin", b(s.postAccountPin))
	mux.HandleFunc("POST /api/v1/accounts/{id}/unpin", b(s.postAccountUnpin))
	mux.HandleFunc("POST /api/v1/accounts/{id}/note", b(s.v1StubBearerEmptyObject))

	mux.HandleFunc("GET /api/v1/accounts/{id}/statuses", s.getAccountStatuses)
	mux.HandleFunc("GET /api/v1/accounts/{id}/followers", s.getAccountFollowers)
	mux.HandleFunc("GET /api/v1/accounts/{id}/following", s.getAccountFollowing)
	mux.HandleFunc("GET /api/v1/accounts/{id}/lists", s.v1StubGETEmptyArray)
	mux.HandleFunc("GET /api/v1/accounts/{id}/identity_proofs", s.v1StubGETEmptyArray)
	mux.HandleFunc("GET /api/v1/accounts/{id}/featured_tags", s.v1StubGETEmptyArray)
	mux.HandleFunc("GET /api/v1/accounts/{id}", s.getAccount)

	mux.HandleFunc("POST /api/v1/statuses", b(s.postStatuses))
	mux.HandleFunc("GET /api/v1/statuses/{id}/context", s.getStatusContext)
	mux.HandleFunc("GET /api/v1/statuses/{id}/favourited_by", s.getStatusFavouritedBy)
	mux.HandleFunc("GET /api/v1/statuses/{id}/reblogged_by", s.getStatusRebloggedBy)
	mux.HandleFunc("POST /api/v1/statuses/{id}/favourite", b(s.postStatusFavourite))
	mux.HandleFunc("POST /api/v1/statuses/{id}/unfavourite", b(s.postStatusUnfavourite))
	mux.HandleFunc("POST /api/v1/statuses/{id}/reblog", b(s.postStatusReblog))
	mux.HandleFunc("POST /api/v1/statuses/{id}/unreblog", b(s.postStatusUnreblog))
	mux.HandleFunc("POST /api/v1/statuses/{id}/bookmark", b(s.postStatusBookmark))
	mux.HandleFunc("POST /api/v1/statuses/{id}/unbookmark", b(s.postStatusUnbookmark))
	mux.HandleFunc("POST /api/v1/statuses/{id}/mute", b(s.v1StubBearerEmptyObject))
	mux.HandleFunc("POST /api/v1/statuses/{id}/unmute", b(s.v1StubBearerEmptyObject))
	mux.HandleFunc("POST /api/v1/statuses/{id}/pin", b(s.postStatusActionNotFound))
	mux.HandleFunc("POST /api/v1/statuses/{id}/unpin", b(s.postStatusActionNotFound))
	mux.HandleFunc("POST /api/v1/statuses/{id}/translate", b(s.postStatusTranslateStub))
	mux.HandleFunc("DELETE /api/v1/statuses/{id}", b(s.deleteStatus))
	mux.HandleFunc("GET /api/v1/statuses/{id}", s.getStatus)

	mux.HandleFunc("GET /api/v1/timelines/home", b(s.getTimelineHome))
	mux.HandleFunc("GET /api/v1/timelines/public", s.getTimelinePublic)
	mux.HandleFunc("GET /api/v1/timelines/tag/{tag}", s.v1StubGETEmptyArray)
	mux.HandleFunc("GET /api/v1/timelines/list/{id}", b(s.getTimelineList))

	mux.HandleFunc("GET /api/v1/tags/{name}", s.getTag)
	mux.HandleFunc("POST /api/v1/tags/{name}/follow", b(s.postTagFollow))
	mux.HandleFunc("POST /api/v1/tags/{name}/unfollow", b(s.postTagUnfollow))

	mux.HandleFunc("GET /api/v1/search", s.getSearchV1)
	mux.HandleFunc("GET /api/v1/directory", s.getDirectory)
	mux.HandleFunc("GET /api/v1/suggestions", s.getSuggestionsV1)
	mux.HandleFunc("GET /api/v1/streaming", s.getStreaming)

	mux.HandleFunc("GET /api/v1/notifications", b(s.getNotifications))
	mux.HandleFunc("GET /api/v1/notifications/unread_count", b(s.getNotificationsUnreadCount))
	mux.HandleFunc("POST /api/v1/notifications/clear", b(s.postNotificationsClear))
	mux.HandleFunc("POST /api/v1/notifications/dismiss", b(s.postNotificationsDismiss))
	mux.HandleFunc("POST /api/v1/notifications/{id}/dismiss", b(s.postNotificationDismissByID))
	mux.HandleFunc("GET /api/v1/notifications/{id}", b(s.getNotificationByID))

	mux.HandleFunc("GET /api/v1/markers", b(s.getMarkers))
	mux.HandleFunc("POST /api/v1/markers", b(s.postMarkers))

	mux.HandleFunc("GET /api/v1/preferences", b(s.getPreferences))
	mux.HandleFunc("GET /api/v1/custom_emojis", s.getCustomEmojis)
	mux.HandleFunc("GET /api/v1/announcements", s.getAnnouncements)
	mux.HandleFunc("POST /api/v1/announcements/{id}/dismiss", b(s.postAnnouncementDismiss))
	mux.HandleFunc("DELETE /api/v1/announcements/{id}/dismiss", b(s.deleteAnnouncementDismiss))

	mux.HandleFunc("GET /api/v1/lists", b(s.getLists))
	mux.HandleFunc("POST /api/v1/lists", b(s.postLists))
	mux.HandleFunc("GET /api/v1/lists/{id}/accounts", b(s.getListAccounts))
	mux.HandleFunc("POST /api/v1/lists/{id}/accounts", b(s.postListAccountsAdd))
	mux.HandleFunc("DELETE /api/v1/lists/{id}/accounts/{account_id}", b(s.deleteListAccountMember))
	mux.HandleFunc("GET /api/v1/lists/{id}", b(s.getListByID))
	mux.HandleFunc("PUT /api/v1/lists/{id}", b(s.putListByID))
	mux.HandleFunc("DELETE /api/v1/lists/{id}", b(s.deleteListByID))

	mux.HandleFunc("POST /api/v1/media", b(s.postAPIMedia))
	mux.HandleFunc("PUT /api/v1/media/{id}", b(s.putAPIMedia))
	mux.HandleFunc("GET /api/v1/media/{id}", s.getAPIMedia)

	mux.HandleFunc("GET /api/v1/polls/{id}", s.getPoll)
	mux.HandleFunc("POST /api/v1/polls/{id}/votes", b(s.postPollVotes))

	mux.HandleFunc("GET /api/v1/filters", b(s.getFiltersV1))
	mux.HandleFunc("POST /api/v1/filters", b(s.postFilters))
	mux.HandleFunc("GET /api/v1/filters/{id}", b(s.getFilterByID))
	mux.HandleFunc("PUT /api/v1/filters/{id}", b(s.putFilterByID))
	mux.HandleFunc("DELETE /api/v1/filters/{id}", b(s.deleteFilterByID))

	mux.HandleFunc("GET /api/v1/follow_requests", s.v1StubGETEmptyArray)
	mux.HandleFunc("POST /api/v1/follow_requests/{id}/authorize", b(s.postFollowRequestAuthorize))
	mux.HandleFunc("POST /api/v1/follow_requests/{id}/reject", b(s.postFollowRequestReject))

	mux.HandleFunc("GET /api/v1/favourites", b(s.getFavourites))
	mux.HandleFunc("GET /api/v1/bookmarks", b(s.getBookmarks))
	mux.HandleFunc("GET /api/v1/mutes", b(s.v1StubBearerEmptyArray))
	mux.HandleFunc("GET /api/v1/blocks", b(s.v1StubBearerEmptyArray))
	mux.HandleFunc("GET /api/v1/domain_blocks", s.v1StubGETEmptyArray)

	mux.HandleFunc("GET /api/v1/trends/tags", s.v1StubGETEmptyArray)
	mux.HandleFunc("GET /api/v1/trends/statuses", s.v1StubGETEmptyArray)
	mux.HandleFunc("GET /api/v1/trends/links", s.v1StubGETEmptyArray)

	mux.HandleFunc("GET /api/v1/push/subscription", b(s.getPushSubscription))
	mux.HandleFunc("POST /api/v1/push/subscription", b(s.postPushSubscription))
	mux.HandleFunc("PUT /api/v1/push/subscription", b(s.putPushSubscription))
	mux.HandleFunc("DELETE /api/v1/push/subscription", b(s.deletePushSubscription))

	mux.HandleFunc("GET /api/v1/scheduled_statuses", b(s.getScheduledStatuses))
	mux.HandleFunc("POST /api/v1/scheduled_statuses", b(s.postScheduledStatuses))
	mux.HandleFunc("GET /api/v1/scheduled_statuses/{id}", b(s.getScheduledStatus))
	mux.HandleFunc("PUT /api/v1/scheduled_statuses/{id}", b(s.putScheduledStatus))
	mux.HandleFunc("DELETE /api/v1/scheduled_statuses/{id}", b(s.deleteScheduledStatus))

	mux.HandleFunc("GET /api/v1/conversations", s.getConversations)
	mux.HandleFunc("POST /api/v1/conversations/read", b(s.postConversationsReadBulk))
	mux.HandleFunc("GET /api/v1/conversations/{id}", b(s.getConversationByID))
	mux.HandleFunc("DELETE /api/v1/conversations/{id}", b(s.deleteConversation))
	mux.HandleFunc("POST /api/v1/conversations/{id}/read", b(s.postConversationReadByID))

	mux.HandleFunc("POST /api/v1/reports", b(s.postReports))

	mux.HandleFunc("GET /api/v1/followed_tags", s.v1StubGETEmptyArray)
	mux.HandleFunc("POST /api/v1/featured_tags", b(s.postFeaturedTag))
	mux.HandleFunc("DELETE /api/v1/featured_tags/{id}", b(s.deleteFeaturedTag))
	mux.HandleFunc("GET /api/v1/endorsements", s.v1StubGETEmptyArray)
	mux.HandleFunc("GET /api/v1/featured_tags", s.v1StubGETEmptyArray)

	for _, method := range []string{
		http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions,
	} {
		mux.HandleFunc(method+" /api/v1/admin/{path...}", s.v1ForbiddenAdmin)
	}
}
