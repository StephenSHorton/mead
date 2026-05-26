export namespace main {
	
	export class BottleSummary {
	    id: string;
	    name: string;
	    created_at?: string;
	    wine_version?: string;
	
	    static createFrom(source: any = {}) {
	        return new BottleSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.created_at = source["created_at"];
	        this.wine_version = source["wine_version"];
	    }
	}
	export class LogsChunk {
	    bytes: string;
	    next_offset: number;
	    exited: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LogsChunk(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.bytes = source["bytes"];
	        this.next_offset = source["next_offset"];
	        this.exited = source["exited"];
	    }
	}
	export class ProcessSummary {
	    run_id: string;
	    bottle_id?: string;
	    argv: string[];
	    started_at: string;
	    log_path?: string;
	    exited: boolean;
	    exited_at?: string;
	    exit_code?: number;
	    run_err?: string;
	
	    static createFrom(source: any = {}) {
	        return new ProcessSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.run_id = source["run_id"];
	        this.bottle_id = source["bottle_id"];
	        this.argv = source["argv"];
	        this.started_at = source["started_at"];
	        this.log_path = source["log_path"];
	        this.exited = source["exited"];
	        this.exited_at = source["exited_at"];
	        this.exit_code = source["exit_code"];
	        this.run_err = source["run_err"];
	    }
	}

}

