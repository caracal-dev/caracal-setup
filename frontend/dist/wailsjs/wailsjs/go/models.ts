export namespace guiapp {
	
	export interface ProfileView {
	    currentUsername: string;
	    currentHome: string;
	}
	export interface SetupRequest {
	    changeAccount: boolean;
	    username: string;
	    password: string;
	}
	export interface SetupResult {
	    appliedUsername: string;
	    appliedHome: string;
	    rebootRequired: boolean;
	}

}

