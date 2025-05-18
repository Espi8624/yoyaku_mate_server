package handlers

import (
	"net/http"
)

func RegisterRoutes(mux *http.ServeMux) {
	// customer side handler 接続
	mux.HandleFunc("/", CustomerHomeHandler)
	mux.HandleFunc("/user-info", UserInfoHandler)
	mux.HandleFunc("/comments-info", UserCommentsHandler)

	mux.HandleFunc("/frequent-store", FrequentStoreHandler)
	mux.HandleFunc("/timeline", UserTimelineHandler)
	mux.HandleFunc("/notifications", NotificationsHandler)

	mux.HandleFunc("/reservations-info", ReservationInfoHandler)
	mux.HandleFunc("/reservation-details", ReservationDetailsByIDHandler)

	mux.HandleFunc("/store-info", StoreInfoHandler)
	mux.HandleFunc("/store-menus", StoreMenuHandler)
	mux.HandleFunc("/store-comments", StoreCommentHandler)
}
