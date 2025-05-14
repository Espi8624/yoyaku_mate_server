package handlers

import (
	"net/http"
)

func RegisterRoutes(mux *http.ServeMux) {
	// customer side handler 接続
	mux.HandleFunc("/home", CustomerHomeHandler)
	mux.HandleFunc("/user-info", UserInfoHandler)
	mux.HandleFunc("/comments-info", UserCommentsHandler)
	mux.HandleFunc("/frequent-store", FrequentStoreHandler)
	mux.HandleFunc("/timeline", UserTimelineHandler)
	mux.HandleFunc("/notifications", NotificationsHandler)

	mux.HandleFunc("/reservations-info", ReservationInfoHandler)

	// provider side handler 接続
	mux.HandleFunc("/provider/home", ProviderHomeHandler)
	mux.HandleFunc("/provider/store-info", StoreInfoHandler)
	mux.HandleFunc("/provider/store-menus", StoreMenuHandler)
	mux.HandleFunc("/provider/store-comments", StoreCommentHandler)
	// mux.HandleFunc("/provider/store-reservations", handlers.StoreReservationsHandler)
}
