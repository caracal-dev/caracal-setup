export namespace guiapp {
	
	export interface ImageOption {
	    label: string;
	    imageName: string;
	    description: string;
	    recommended: boolean;
	}
	export interface ProfileView {
	    currentUsername: string;
	    currentHome: string;
	    currentHostname: string;
	}
	export interface SetupRequest {
	
	}
	export interface SetupResult {
	    appliedUsername: string;
	    appliedHome: string;
	    appliedHostname: string;
	    rebootRequired: boolean;
	}

}

