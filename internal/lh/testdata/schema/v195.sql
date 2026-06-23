CREATE TABLE version(
        id INTEGER PRIMARY KEY,
        version INTEGER NOT NULL,
        UNIQUE (version)
    );
CREATE TABLE lh_users (
        id INTEGER PRIMARY KEY,
        lh_user_id INTEGER NOT NULL,
        created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
        updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
        last_login_at DATETIME NOT NULL,
        UNIQUE(lh_user_id)
    );
CREATE INDEX lh_users_last_login_at_idx ON lh_users(last_login_at);
CREATE TABLE li_accounts (
        id INTEGER PRIMARY KEY,
        li_account_id INTEGER NOT NULL,
        created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
        updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
        last_login_at DATETIME NOT NULL,
        UNIQUE(li_account_id)
    );
CREATE INDEX li_accounts_last_login_at_idx ON li_accounts(last_login_at);
CREATE TABLE disabled_triggers(
    id INTEGER PRIMARY KEY,
    trigger_name TEXT,
    UNIQUE (trigger_name)
);
CREATE TABLE collect_info(
    id INTEGER PRIMARY KEY,
    collecting_source TEXT NOT NULL,
    collecting_scope_id TEXT,
    collecting_scope_type TEXT,
    li_account_id INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (li_account_id) REFERENCES li_accounts(id)
);
CREATE INDEX collect_info_li_account_id_idx ON collect_info(li_account_id);
CREATE TABLE campaigns(
    id INTEGER PRIMARY KEY,
    name TEXT,
    description TEXT,
    type INTEGER NOT NULL,
    is_paused INTEGER DEFAULT 1,
    is_archived INTEGER DEFAULT 0,
    is_valid INTEGER,
    li_account_id INTEGER NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    FOREIGN KEY(li_account_id) REFERENCES li_accounts(id)
);
CREATE INDEX campaigns_li_account_id_type_idx ON campaigns(li_account_id,type);
CREATE TABLE people(
    id INTEGER PRIMARY KEY,
    original_id INTEGER,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW'))
);
CREATE INDEX people_original_id_idx ON people(original_id);
CREATE TABLE deduplications(
    id INTEGER PRIMARY KEY,
    original_person_id INTEGER NOT NULL,
    duplicated_person_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    FOREIGN KEY (original_person_id) REFERENCES people(id),
    FOREIGN KEY (duplicated_person_id) REFERENCES people(id)
);
CREATE TABLE action_configs(
    id INTEGER PRIMARY KEY,
    "actionType" TEXT NOT NULL,
    "actionSettings" TEXT,
    "coolDown" INTEGER NOT NULL,
    "maxActionResultsPerIteration" BIGINT NOT NULL,
    "isDraft" INTEGER DEFAULT 1,
    override_platform TEXT
);
CREATE INDEX action_configs_actionType_idx
ON action_configs("actionType");
CREATE TABLE actions(
    id INTEGER PRIMARY KEY,
    campaign_id INTEGER,
    name TEXT,
    description TEXT,
    "startAt" DATETIME NOT NULL,
    postpone_reason TEXT,
    postpone_reason_data TEXT,
    FOREIGN KEY(campaign_id) REFERENCES campaigns(id)
);
CREATE TABLE action_versions(
    id INTEGER PRIMARY KEY,
    action_id INTEGER,
    config_id INTEGER,
    exclude_list_id INTEGER,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    FOREIGN KEY(action_id) REFERENCES actions(id),
    FOREIGN KEY(config_id) REFERENCES action_configs(id)
);
CREATE INDEX action_versions_config_id_idx
ON action_versions(config_id);
CREATE INDEX action_versions_action_id_config_id_idx
ON action_versions(action_id, config_id);
CREATE TABLE action_results(
    id INTEGER PRIMARY KEY,
    action_version_id INTEGER NOT NULL,
    action_iteration_id INTEGER,
    person_id INTEGER NOT NULL,
    result INTEGER NOT NULL DEFAULT 0,
    data TEXT,
    deduplication_id INTEGER,
    original_id INTEGER,
    platform TEXT,
    target_platform TEXT NOT NULL,
    invited_platform TEXT,
    messaged_platform TEXT,
    li_account_id INTEGER NOT NULL,
    created_at DATETIME NOT NULL,
    UNIQUE(action_version_id, person_id),
    FOREIGN KEY(action_version_id) REFERENCES action_versions(id),
    FOREIGN KEY(person_id) REFERENCES people(id),
    FOREIGN KEY(deduplication_id) REFERENCES deduplications(id),
    FOREIGN KEY(original_id) REFERENCES action_results(id),
    FOREIGN KEY(li_account_id) REFERENCES li_accounts(id)
);
CREATE INDEX action_results_created_at_idx
ON action_results (created_at);
CREATE INDEX action_results_action_iteration_id_idx
ON action_results(action_iteration_id);
CREATE INDEX action_results_action_iteration_id_result_idx
ON action_results(action_iteration_id, result);
CREATE INDEX action_results_result_created_at_idx
ON action_results(result, created_at);
CREATE INDEX action_results_person_id_idx
ON action_results(person_id);
CREATE TABLE action_result_flags(
    id INTEGER PRIMARY KEY,
    action_result_id INTEGER NOT NULL,
    is_exception INTEGER NOT NULL DEFAULT 1,
    who_to_blame TEXT NOT NULL DEFAULT 'LH',
    is_retryable INTEGER NOT NULL DEFAULT 0,
    code INTEGER,
    recipient_replied INTEGER DEFAULT 0,
    sender_messaged INTEGER DEFAULT 0,
    FOREIGN KEY(action_result_id) REFERENCES action_results(id)
);
CREATE INDEX action_result_flags_action_result_id_idx
ON action_result_flags(action_result_id);
CREATE TABLE action_target_people(
    id INTEGER PRIMARY KEY,
    action_id INTEGER NOT NULL,
    action_version_id INTEGER NOT NULL,
    person_id INTEGER NOT NULL,
    state INTEGER NOT NULL,
    created_at DATETIME NOT NULL,
    collect_id INTEGER,
    deduplication_id INTEGER,
    override_platform TEXT,
    collecting_scope_id TEXT,
    collecting_scope_type TEXT,
    invited_platform TEXT,
    messaged_platform TEXT,
    prev_action_target_platform TEXT,
    li_account_id INTEGER NOT NULL,
    UNIQUE(action_version_id, person_id),
    FOREIGN KEY(action_id) REFERENCES actions(id),
    FOREIGN KEY(action_version_id) REFERENCES action_versions(id),
    FOREIGN KEY(person_id) REFERENCES people(id),
    FOREIGN KEY(collect_id) REFERENCES collect_info(id),
    FOREIGN KEY(deduplication_id) REFERENCES deduplications(id),
    FOREIGN KEY(li_account_id) REFERENCES li_accounts(id)
);
CREATE INDEX action_target_people_action_id_person_id_idx
ON action_target_people(action_id, person_id);
CREATE INDEX action_target_people_action_id_action_version_id_idx
ON action_target_people(action_id, action_version_id);
CREATE INDEX action_target_people_action_id_person_id_action_version_id_idx
ON action_target_people(action_id, person_id, action_version_id);
CREATE INDEX action_target_people_person_id_collect_id_idx
ON action_target_people(person_id ASC, collect_id DESC);
CREATE TABLE campaign_versions(
    id INTEGER PRIMARY KEY,
    campaign_id INTEGER,
    exclude_list_id INTEGER,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    FOREIGN KEY(campaign_id) REFERENCES campaigns(id)
);
CREATE INDEX campaign_versions_campaign_id_idx ON campaign_versions(campaign_id);
CREATE TABLE campaign_version_actions(
    id INTEGER PRIMARY KEY,
    version_id INTEGER NOT NULL,
    action_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE(version_id, action_id),
    FOREIGN KEY(version_id) REFERENCES campaign_versions(id),
    FOREIGN KEY(action_id) REFERENCES actions(id)
);
CREATE INDEX campaign_version_actions_action_id_idx
ON campaign_version_actions(action_id);
CREATE TABLE person_external_ids(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    external_id TEXT NOT NULL,
    external_id_uppercase TEXT NOT NULL,
    type_group TEXT NOT NULL CHECK(type_group IN ('member', 'public', 'hash', 'avatar')),
    is_member_id INTEGER,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE(external_id, type_group),
    UNIQUE(person_id, is_member_id),
    FOREIGN KEY(person_id) REFERENCES people(id)
);
CREATE INDEX person_external_ids_person_idx ON person_external_ids(person_id);
CREATE INDEX person_external_ids_external_id_uppercase_type_group_ids ON person_external_ids(external_id_uppercase, type_group);
CREATE UNIQUE INDEX person_external_ids_person_id_is_member_id_idx ON person_external_ids(person_id, is_member_id);
CREATE TABLE person_email(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    email TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('personal', 'business')),
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    UNIQUE (person_id, email),
    FOREIGN KEY(person_id) REFERENCES people(id)
);
CREATE INDEX person_email_person_id_sent_at_to_pas_idx
ON person_email(person_id, sent_at_to_pas) WHERE sent_at_to_pas IS NULL;
CREATE TABLE person_network_info(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    following INTEGER,
    followable INTEGER,
    li_account_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    actual_at DATETIME NOT NULL,
    UNIQUE(person_id, li_account_id),
    FOREIGN KEY(person_id) REFERENCES people(id),
    FOREIGN KEY(li_account_id) REFERENCES li_accounts(id)
);
CREATE TABLE person_original_current_position (
        id INTEGER PRIMARY KEY,
        person_id INTEGER NOT NULL,
        company TEXT NOT NULL,
        position TEXT NOT NULL,
        created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
        updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
        sent_at_to_pas DATETIME,
        actual_at DATETIME NOT NULL,
        UNIQUE(person_id),
        FOREIGN KEY(person_id) REFERENCES people(id)
);
CREATE INDEX person_original_current_position_person_id_sent_at_to_pas_idx
ON person_original_current_position(person_id, sent_at_to_pas) WHERE sent_at_to_pas IS NULL;
CREATE TABLE tags(
    id INTEGER PRIMARY KEY,
    title TEXT NOT NULL,
    title_uppercase TEXT NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE (title_uppercase)
);
CREATE TABLE person_tag(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    tag_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE (person_id, tag_id),
    FOREIGN KEY(person_id) REFERENCES people(id),
    FOREIGN KEY(tag_id) REFERENCES tags(id) ON DELETE CASCADE
);
CREATE INDEX person_tag_tag_id_idx
ON person_tag(tag_id);
CREATE TABLE skills(
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE (name)
);
CREATE TABLE person_skill(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    skill_id INTEGER NOT NULL,
    endorsements_count INTEGER,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    UNIQUE (person_id, skill_id),
    FOREIGN KEY(person_id) REFERENCES people(id),
    FOREIGN KEY(skill_id) REFERENCES skills(id)
);
CREATE INDEX person_skill_person_id_sent_at_to_pas_idx
ON person_skill(person_id, sent_at_to_pas) WHERE sent_at_to_pas IS NULL;
CREATE INDEX person_skill_skill_id_idx ON person_skill(skill_id);
CREATE TABLE person_interests(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    type INTEGER NOT NULL,
    external_public_id TEXT NOT NULL,
    name TEXT NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE(person_id, type, external_public_id),
    FOREIGN KEY(person_id) REFERENCES people(id)
);
CREATE TABLE person_recommendations(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    type INTEGER NOT NULL,
    content TEXT NOT NULL,
    another_person_external_public_id TEXT NOT NULL,
    name TEXT NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE (person_id, type, another_person_external_public_id),
    FOREIGN KEY(person_id) REFERENCES people(id)
);
CREATE TABLE person_positions(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    title TEXT NOT NULL,
    company_name TEXT NOT NULL,
    company_id TEXT,
    start DATETIME,
    start_year INTEGER,
    start_month INTEGER,
    "end" DATETIME,
    end_year INTEGER,
    end_month INTEGER,
    location_name TEXT,
    description TEXT,
    is_default INTEGER,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    UNIQUE(person_id, is_default),
    FOREIGN KEY(person_id) REFERENCES people(id),
    CHECK(is_default IS NULL OR (is_default IS NOT NULL AND "end" IS NULL))
);
CREATE INDEX person_positions_person_id_sent_at_to_pas_idx
ON person_positions(person_id, sent_at_to_pas) WHERE sent_at_to_pas IS NULL;
CREATE TABLE person_education(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    school_name TEXT NOT NULL,
    degree_name TEXT,
    field_of_study TEXT,
    description TEXT,
    start_year INTEGER,
    start_month INTEGER,
    end_year INTEGER,
    end_month INTEGER,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    FOREIGN KEY(person_id) REFERENCES people(id)
);
CREATE INDEX person_education_person_idx ON person_education(person_id);
CREATE INDEX person_education_person_id_sent_at_to_pas_idx
ON person_education(person_id, sent_at_to_pas) WHERE sent_at_to_pas IS NULL;
CREATE TABLE person_mini_profile(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    first_name TEXT NOT NULL,
    first_name_uppercase TEXT,
    last_name TEXT,
    last_name_uppercase TEXT,
    headline TEXT,
    headline_uppercase TEXT,
    avatar TEXT,
    first_mutual_full_name TEXT,
    second_mutual_full_name TEXT,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE (person_id),
    FOREIGN KEY(person_id) REFERENCES people(id)
);
CREATE TABLE person_member_distance(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    distance TEXT NOT NULL,
    li_account_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    actual_at DATETIME NOT NULL,
    UNIQUE(person_id, li_account_id),
    FOREIGN KEY(person_id) REFERENCES people(id),
    FOREIGN KEY(li_account_id) REFERENCES li_accounts(id)
);
CREATE TABLE person_custom_fields(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    campaign_id INTEGER,
    campaign_id_uniq_constrain INTEGER NOT NULL DEFAULT 0,
    action_id INTEGER,
    action_id_uniq_constrain INTEGER NOT NULL DEFAULT 0,
    level TEXT NOT NULL,
    field_name TEXT NOT NULL,
    field_content TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    UNIQUE (person_id, campaign_id_uniq_constrain, action_id_uniq_constrain, field_name),
    FOREIGN KEY(person_id) REFERENCES people(id),
    FOREIGN KEY(campaign_id) REFERENCES campaigns(id),
    FOREIGN KEY(action_id) REFERENCES actions(id)
);
CREATE INDEX person_custom_fields_person_id_campaign_id_filed_name_idx
ON person_custom_fields(person_id, campaign_id, field_name);
CREATE INDEX person_custom_fields_person_id_action_id_filed_name_idx
ON person_custom_fields(person_id, action_id, field_name);
CREATE INDEX person_custom_fields_person_id_filed_name_level_idx
ON person_custom_fields(person_id, field_name, level);
CREATE INDEX person_custom_fields_campaign_id_uniq_constrain_action_id_uniq_constrain_field_name_idx
ON person_custom_fields(campaign_id_uniq_constrain, action_id_uniq_constrain, field_name);
CREATE TABLE person_original_mini_profile(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    first_name TEXT NOT NULL,
    last_name TEXT,
    full_name TEXT NOT NULL,
    headline TEXT,
    avatar TEXT,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    UNIQUE (person_id),
    FOREIGN KEY(person_id) REFERENCES people(id)
);
CREATE INDEX person_original_mini_profile_person_id_sent_at_to_pas_idx
ON person_original_mini_profile(person_id, sent_at_to_pas) WHERE sent_at_to_pas IS NULL;
CREATE TABLE person_certifications(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    authority TEXT NOT NULL,
    start DATETIME,
    start_year INTEGER,
    start_month INTEGER,
    "end" DATETIME,
    end_year INTEGER,
    end_month INTEGER,
    license_number TEXT,
    display_source TEXT,
    url TEXT,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    FOREIGN KEY(person_id) REFERENCES people(id)
);
CREATE INDEX person_certifications_person_idx ON person_certifications(person_id);
CREATE TABLE person_volunteers(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    role TEXT NOT NULL,
    cause TEXT,
    description TEXT,
    company_id INTEGER,
    company_public_id TEXT NOT NULL,
    company_name TEXT NOT NULL,
    started_at TEXT NOT NULL,
    ended_at TEXT,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE (person_id, company_name, role, started_at),
    FOREIGN KEY(person_id) REFERENCES people(id)
);
CREATE TABLE person_current_position (
        id INTEGER PRIMARY KEY,
        person_id INTEGER NOT NULL,
        company TEXT,
        company_uppercase TEXT,
        position TEXT,
        position_uppercase TEXT,
        created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
        updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
        UNIQUE(person_id),
        FOREIGN KEY(person_id) REFERENCES people(id)
    );
CREATE TABLE person_custom_current_position (
        id INTEGER PRIMARY KEY,
        person_id INTEGER NOT NULL,
        company TEXT,
        company_outdated INTEGER DEFAULT 0,
        position TEXT,
        position_outdated INTEGER DEFAULT 0,
        created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
        updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
        UNIQUE(person_id),
        FOREIGN KEY(person_id) REFERENCES people(id)
    );
CREATE TABLE person_note(
        id INTEGER PRIMARY KEY,
        person_id INTEGER NOT NULL,
        note TEXT CHECK(note IS NULL OR LENGTH(note) <= 100000),
        created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
        updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
        UNIQUE(person_id),
        FOREIGN KEY(person_id) REFERENCES people(id)
    );
CREATE TABLE person_custom_mini_profile(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    first_name TEXT,
    first_name_outdated INTEGER DEFAULT 0,
    last_name TEXT,
    last_name_outdated INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE (person_id),
    FOREIGN KEY(person_id) REFERENCES people(id)
);
CREATE TABLE person_languages(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    proficiency TEXT,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    FOREIGN KEY(person_id) REFERENCES people(id)
);
CREATE INDEX person_languages_person_idx ON person_languages(person_id);
CREATE INDEX person_languages_person_id_sent_at_to_pas_idx
ON person_languages(person_id, sent_at_to_pas) WHERE sent_at_to_pas IS NULL;
CREATE TABLE person_mutual_total(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    mutual_total INTEGER NOT NULL,
    li_account_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    UNIQUE (person_id, li_account_id),
    FOREIGN KEY(person_id) REFERENCES people(id),
    FOREIGN KEY(li_account_id) REFERENCES li_accounts(id)
);
CREATE TABLE person_external_id_identifiers(
    id INTEGER PRIMARY KEY,
    person_external_id INTEGER NOT NULL,
    type TEXT NOT NULL,
    hash TEXT,
    hash_normalized TEXT NOT NULL,
    auth_type TEXT,
    auth_type_normalized TEXT NOT NULL,
    auth_token TEXT,
    auth_token_normalized TEXT NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    UNIQUE(
        person_external_id,
        type,
        hash_normalized,
        auth_type_normalized,
        auth_token_normalized
    ),
    FOREIGN KEY(person_external_id) REFERENCES person_external_ids(id)
);
CREATE INDEX person_external_id_identifiers_person_external_id_sent_at_to_pas_idx
ON person_external_id_identifiers(person_external_id, sent_at_to_pas) WHERE sent_at_to_pas IS NULL;
CREATE TABLE working_intervals(
    id INTEGER PRIMARY KEY,
    campaign_id INTEGER,
    action_id INTEGER,
    working_week_day INTEGER NOT NULL,
    day_and_night INTEGER NOT NULL DEFAULT 0,
    started_at INTEGER,
    ended_at INTEGER,
    li_account_id INTEGER NOT NULL,
    UNIQUE(li_account_id, campaign_id, action_id, working_week_day, started_at, ended_at),
    FOREIGN KEY(campaign_id) REFERENCES campaigns(id),
    FOREIGN KEY(action_id) REFERENCES actions(id),
    FOREIGN KEY(li_account_id) REFERENCES li_accounts(id)
);
CREATE TABLE daily_limits(
    id INTEGER PRIMARY KEY,
    li_account_id INTEGER NOT NULL,
    max_limit INTEGER NOT NULL DEFAULT 150,
    UNIQUE(li_account_id),
    FOREIGN KEY(li_account_id) REFERENCES li_accounts(id)
);
CREATE TABLE collections(
    id INTEGER PRIMARY KEY,
    name TEXT,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE(name)
);
CREATE TABLE collection_people(
    id INTEGER PRIMARY KEY,
    collection_id INTEGER NOT NULL,
    person_id INTEGER NOT NULL,
    collect_id INTEGER,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE (collection_id, person_id),
    FOREIGN KEY(collection_id) REFERENCES collections(id),
    FOREIGN KEY(person_id) REFERENCES people(id),
    FOREIGN KEY(collect_id) REFERENCES collect_info(id)
);
CREATE INDEX collection_people_person_id_idx
ON collection_people(person_id);
CREATE INDEX collection_people_collect_id_idx
ON collection_people(collect_id);
CREATE TABLE person_badges(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    premium INTEGER DEFAULT NULL,
    influencer INTEGER DEFAULT NULL,
    open_link INTEGER DEFAULT NULL,
    job_seeker INTEGER DEFAULT NULL,
    hiring INTEGER DEFAULT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    UNIQUE (person_id),
    FOREIGN KEY(person_id) REFERENCES people(id)
);
CREATE INDEX person_badges_premium_idx ON person_badges(premium);
CREATE INDEX person_badges_influencer_idx ON person_badges(influencer);
CREATE INDEX person_badges_open_link_idx ON person_badges(open_link);
CREATE INDEX person_badges_job_seeker_idx ON person_badges(job_seeker);
CREATE INDEX person_badges_hiring_idx ON person_badges(hiring);
CREATE INDEX person_badges_person_id_sent_at_to_pas_idx
ON person_badges(person_id, sent_at_to_pas) WHERE sent_at_to_pas IS NULL;
CREATE TABLE person_mutual(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    first_mutual_full_name TEXT,
    second_mutual_full_name TEXT,
    li_account_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE (person_id, li_account_id),
    FOREIGN KEY(person_id) REFERENCES people(id),
    FOREIGN KEY(li_account_id) REFERENCES li_accounts(id)
);
CREATE TABLE person_custom_mutual(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    first_mutual_full_name TEXT,
    second_mutual_full_name TEXT,
    li_account_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE (person_id, li_account_id),
    FOREIGN KEY(person_id) REFERENCES people(id),
    FOREIGN KEY(li_account_id) REFERENCES li_accounts(id)
);
CREATE TABLE person_original_mutual(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    first_mutual_full_name TEXT,
    second_mutual_full_name TEXT,
    li_account_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    UNIQUE (person_id, li_account_id),
    FOREIGN KEY(person_id) REFERENCES people(id),
    FOREIGN KEY(li_account_id) REFERENCES li_accounts(id)
);
CREATE TABLE person_followers(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    count INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    UNIQUE (person_id),
    FOREIGN KEY(person_id) REFERENCES people(id)
);
CREATE INDEX person_followers_person_id_sent_at_to_pas_idx
ON person_followers(person_id, sent_at_to_pas) WHERE sent_at_to_pas IS NULL;
CREATE TABLE person_address(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    address TEXT,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    UNIQUE (person_id),
    FOREIGN KEY(person_id) REFERENCES people(id)
);
CREATE INDEX person_address_person_id_sent_at_to_pas_idx
ON person_address(person_id, sent_at_to_pas) WHERE sent_at_to_pas IS NULL;
CREATE TABLE person_birthday(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    month INTEGER NOT NULL,
    day INTEGER NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    UNIQUE (person_id),
    FOREIGN KEY(person_id) REFERENCES people(id)
);
CREATE INDEX person_birthday_person_id_sent_at_to_pas_idx
ON person_birthday(person_id, sent_at_to_pas) WHERE sent_at_to_pas IS NULL;
CREATE TABLE person_connect(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    connected_at BIGINT NOT NULL,
    li_account_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    actual_at DATETIME NOT NULL,
    UNIQUE (person_id, li_account_id),
    FOREIGN KEY(person_id) REFERENCES people(id),
    FOREIGN KEY(li_account_id) REFERENCES li_accounts(id)
);
CREATE TABLE person_connections_info(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    connections_count INTEGER NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    UNIQUE(person_id),
    FOREIGN KEY(person_id) REFERENCES people(id)
);
CREATE INDEX person_connections_info_person_id_sent_at_to_pas_idx
ON person_connections_info(person_id, sent_at_to_pas) WHERE sent_at_to_pas IS NULL;
CREATE TABLE person_websites(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    type TEXT,
    url TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT 'linkedin',
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    UNIQUE(person_id, type, url, source),
    FOREIGN KEY(person_id) REFERENCES people(id)
);
CREATE INDEX person_websites_person_id_sent_at_to_pas_idx
ON person_websites(person_id, sent_at_to_pas) WHERE sent_at_to_pas IS NULL;
CREATE TABLE person_twitters(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT 'linkedin',
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    UNIQUE(person_id, name, source),
    FOREIGN KEY(person_id) REFERENCES people(id)
);
CREATE INDEX person_twitters_person_idx ON person_twitters(person_id);
CREATE INDEX person_twitters_person_id_sent_at_to_pas_idx
ON person_twitters(person_id, sent_at_to_pas) WHERE sent_at_to_pas IS NULL;
CREATE TABLE person_messengers(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    provider TEXT NOT NULL,
    messenger_id TEXT NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    CONSTRAINT UQ_person_messengers_person_id_and_provider_and_messenger_id UNIQUE(person_id, provider, messenger_id),
    CONSTRAINT FK_person_messengers_person_id FOREIGN KEY(person_id) REFERENCES people(id)
);
CREATE INDEX person_messengers_person_id_sent_at_to_pas_idx
ON person_messengers(person_id, sent_at_to_pas) WHERE sent_at_to_pas IS NULL;
CREATE TABLE person_phone_numbers(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    type TEXT NOT NULL,
    number TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT 'linkedin',
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    CONSTRAINT UQ_person_phone_numbers_person_id_and_type_and_number UNIQUE(person_id, type, number, source),
    CONSTRAINT FK_person_phone_numbers_person_id FOREIGN KEY(person_id) REFERENCES people(id)
);
CREATE INDEX person_phone_numbers_person_idx ON person_phone_numbers(person_id);
CREATE INDEX person_phone_numbers_person_id_sent_at_to_pas_idx
ON person_phone_numbers(person_id, sent_at_to_pas) WHERE sent_at_to_pas IS NULL;
CREATE TABLE person_in_campaigns_history(
    id INTEGER PRIMARY KEY,
    action_target_people_id INTEGER NOT NULL,
    action_target_action_version_id INTEGER NOT NULL,
    person_id INTEGER NOT NULL,
    person_collect_id INTEGER,
    campaign_id INTEGER,
    action_id INTEGER,
    action_order_id INTEGER NOT NULL DEFAULT -999,
    result_id INTEGER,
    result_action_version_id INTEGER,
    result_action_iteration_id INTEGER,
    result_status INTEGER NOT NULL DEFAULT -999,
    result_data TEXT,
    result_data_message TEXT,
    result_platform TEXT,
    result_target_platform TEXT,
    result_flags_id INTEGER,
    result_is_exception INTEGER,
    result_who_to_blame TEXT,
    result_is_retryable INTEGER,
    result_code INTEGER,
    result_flag_recipient_replied INTEGER,
    result_flag_sender_messaged INTEGER,
    result_invited_platform TEXT,
    result_messaged_platform TEXT,
    result_created_at DATETIME,
    add_to_target_date DATETIME,
    add_to_target_or_result_saved_date DATETIME,
    action_add_to_target_state INTEGER NOT NULL,
    override_platform TEXT,
    collecting_scope_id TEXT,
    collecting_scope_type TEXT,
    invited_platform TEXT,
    messaged_platform TEXT,
    prev_action_target_platform TEXT,
    action_target_li_account_id INTEGER NOT NULL,
    result_li_account_id INTEGER,
    UNIQUE (person_id, action_id),
    FOREIGN KEY(action_target_people_id) REFERENCES action_target_people(id),
    FOREIGN KEY(action_target_action_version_id) REFERENCES action_versions(id),
    FOREIGN KEY(person_id) REFERENCES people(id),
    FOREIGN KEY(campaign_id) REFERENCES campaigns(id),
    FOREIGN KEY(action_id) REFERENCES actions(id),
    FOREIGN KEY(result_id) REFERENCES action_results(id),
    FOREIGN KEY(person_collect_id) REFERENCES collect_info(id),
    FOREIGN KEY(action_target_li_account_id) REFERENCES li_accounts(id),
    FOREIGN KEY(result_li_account_id) REFERENCES li_accounts(id)
);
CREATE INDEX person_in_campaigns_history_action_id_action_target_action_version_id_idx
ON person_in_campaigns_history (action_id, action_target_action_version_id);
CREATE INDEX person_in_campaigns_history_action_id_person_id_result_status_idx
ON person_in_campaigns_history (action_id, person_id, result_status);
CREATE INDEX person_in_campaigns_history_action_id_result_status_action_target_action_version_id_idx
ON person_in_campaigns_history (action_id, result_status, action_target_action_version_id);
CREATE INDEX person_in_campaigns_history_campaign_id_action_id_idx
ON person_in_campaigns_history (campaign_id, action_id);
CREATE INDEX person_in_campaigns_history_campaign_id_person_id_result_status_action_add_to_target_state_action_order_id_idx
ON person_in_campaigns_history (campaign_id, person_id, result_status, action_add_to_target_state, action_order_id);
CREATE INDEX person_in_campaigns_history_campaign_id_result_status_action_add_to_target_state_action_order_id_idx
ON person_in_campaigns_history (campaign_id, result_status, action_add_to_target_state, action_order_id);
CREATE INDEX person_in_campaigns_history_result_id_idx
ON person_in_campaigns_history (result_id);
CREATE TABLE organizations(
    id INTEGER PRIMARY KEY,
    original_id INTEGER,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW'))
);
CREATE INDEX organizations_original_id_idx ON organizations(original_id);
CREATE TABLE organization_tag(
    id INTEGER PRIMARY KEY,
    organization_id INTEGER NOT NULL,
    tag_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE (organization_id, tag_id),
    FOREIGN KEY(organization_id) REFERENCES organizations(id),
    FOREIGN KEY(tag_id) REFERENCES tags(id) ON DELETE CASCADE
);
CREATE INDEX organization_tag_tag_id_idx
ON organization_tag(tag_id);
CREATE TABLE industries(
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    name_uppercase TEXT NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE (name_uppercase)
);
CREATE TABLE specialities(
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    name_uppercase TEXT NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE (name_uppercase)
);
CREATE TABLE organization_external_ids(
    id INTEGER PRIMARY KEY,
    organization_id INTEGER NOT NULL,
    external_id TEXT NOT NULL,
    external_id_uppercase TEXT NOT NULL,
    type_group TEXT NOT NULL CHECK(type_group IN ('company', 'public')),
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE(external_id, type_group),
    FOREIGN KEY(organization_id) REFERENCES organizations(id)
);
CREATE INDEX organization_external_ids_organization_idx ON organization_external_ids(organization_id);
CREATE INDEX organization_external_ids_external_id_uppercase_type_group_ids ON organization_external_ids(external_id_uppercase, type_group);
CREATE TABLE organization_external_id_identifiers(
    id INTEGER PRIMARY KEY,
    organization_external_id INTEGER NOT NULL,
    type TEXT NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    UNIQUE(
        organization_external_id,
        type
    ),
    FOREIGN KEY(organization_external_id) REFERENCES organization_external_ids(id)
);
CREATE INDEX organization_external_id_identifiers_organization_external_id_sent_at_to_pas_idx
ON organization_external_id_identifiers(organization_external_id, sent_at_to_pas) WHERE sent_at_to_pas IS NULL;
CREATE TABLE organization_mini_profile(
    id INTEGER PRIMARY KEY,
    organization_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    name_uppercase TEXT,
    logo TEXT,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    UNIQUE (organization_id),
    FOREIGN KEY(organization_id) REFERENCES organizations(id)
);
CREATE INDEX organization_mini_profile_organization_id_sent_at_to_pas_idx
    ON organization_mini_profile(organization_id, sent_at_to_pas) WHERE sent_at_to_pas IS NULL;
CREATE TABLE organization_extra(
    id INTEGER PRIMARY KEY,
    organization_id INTEGER NOT NULL,
    type TEXT,
    description TEXT,
    tagline TEXT,
    website TEXT,
    phone TEXT,
    staff_count INTEGER,
    staff_count_start INTEGER,
    staff_count_end INTEGER,
    follower_count INTEGER,
    founded_on INTEGER,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    UNIQUE (organization_id),
    FOREIGN KEY(organization_id) REFERENCES organizations(id)
);
CREATE INDEX organization_extra_organization_id_sent_at_to_pas_idx
    ON organization_extra(organization_id, sent_at_to_pas) WHERE sent_at_to_pas IS NULL;
CREATE TABLE organization_headquarter_address(
    id INTEGER PRIMARY KEY,
    organization_id INTEGER NOT NULL,
    full_address TEXT,
    country TEXT,
    geographic_area TEXT,
    city TEXT,
    postal_code TEXT,
    line1 TEXT,
    line2 TEXT,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    UNIQUE (organization_id),
    FOREIGN KEY(organization_id) REFERENCES organizations(id)
);
CREATE TABLE organization_industries(
    id INTEGER PRIMARY KEY,
    organization_id INTEGER NOT NULL,
    industry_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    UNIQUE (organization_id, industry_id),
    FOREIGN KEY(organization_id) REFERENCES organizations(id),
    FOREIGN KEY(industry_id) REFERENCES industries(id) ON DELETE CASCADE
);
CREATE INDEX organization_industries_industry_id_idx ON organization_industries(industry_id);
CREATE INDEX organization_industries_organization_id_sent_at_to_pas_idx
    ON organization_industries(organization_id, sent_at_to_pas) WHERE sent_at_to_pas IS NULL;
CREATE TABLE organization_specialities(
    id INTEGER PRIMARY KEY,
    organization_id INTEGER NOT NULL,
    speciality_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    UNIQUE (organization_id, speciality_id),
    FOREIGN KEY(organization_id) REFERENCES organizations(id),
    FOREIGN KEY(speciality_id) REFERENCES specialities(id) ON DELETE CASCADE
);
CREATE INDEX organization_specialities_organization_id_sent_at_to_pas_idx
    ON organization_specialities(organization_id, sent_at_to_pas) WHERE sent_at_to_pas IS NULL;
CREATE TABLE collection_organizations(
    id INTEGER PRIMARY KEY,
    collection_id INTEGER NOT NULL,
    organization_id INTEGER NOT NULL,
    collect_id INTEGER,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE (collection_id, organization_id),
    FOREIGN KEY(collection_id) REFERENCES collections(id),
    FOREIGN KEY(organization_id) REFERENCES organizations(id),
    FOREIGN KEY(collect_id) REFERENCES collect_info(id)
);
CREATE INDEX collection_organizations_organization_id_idx
ON collection_organizations(organization_id);
CREATE INDEX collection_organizations_collect_id_idx
ON collection_organizations(collect_id);
CREATE TABLE deduplications_organizations(
    id INTEGER PRIMARY KEY,
    original_organization_id INTEGER NOT NULL,
    duplicated_organization_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    FOREIGN KEY (original_organization_id) REFERENCES organizations(id),
    FOREIGN KEY (duplicated_organization_id) REFERENCES organizations(id)
);
CREATE TABLE action_target_organizations(
    id INTEGER PRIMARY KEY,
    action_id INTEGER NOT NULL,
    action_version_id INTEGER NOT NULL,
    organization_id INTEGER NOT NULL,
    state INTEGER NOT NULL,
    created_at DATETIME NOT NULL,
    collect_id INTEGER,
    deduplication_id INTEGER,
    override_platform TEXT,
    collecting_scope_id TEXT,
    collecting_scope_type TEXT,
    prev_action_target_platform TEXT,
    li_account_id INTEGER NOT NULL,
    UNIQUE(action_version_id, organization_id),
    FOREIGN KEY(action_id) REFERENCES actions(id),
    FOREIGN KEY(action_version_id) REFERENCES action_versions(id),
    FOREIGN KEY(organization_id) REFERENCES organizations(id),
    FOREIGN KEY(collect_id) REFERENCES collect_info(id),
    FOREIGN KEY(deduplication_id) REFERENCES deduplications_organizations(id),
    FOREIGN KEY(li_account_id) REFERENCES li_accounts(id)
);
CREATE INDEX action_target_organizations_action_id_organization_id_idx
ON action_target_organizations(action_id, organization_id);
CREATE INDEX action_target_organizations_action_id_action_version_id_idx
ON action_target_organizations(action_id, action_version_id);
CREATE INDEX action_target_organizations_action_id_organization_id_action_version_id_idx
ON action_target_organizations(action_id, organization_id, action_version_id);
CREATE INDEX action_target_organizations_person_id_collect_id_idx
ON action_target_organizations(organization_id ASC, collect_id DESC);
CREATE TABLE organizations_action_results(
    id INTEGER PRIMARY KEY,
    action_version_id INTEGER NOT NULL,
    action_iteration_id INTEGER,
    organization_id INTEGER NOT NULL,
    result INTEGER NOT NULL DEFAULT 0,
    data TEXT,
    deduplication_id INTEGER,
    original_id INTEGER,
    created_at DATETIME NOT NULL,
    platform TEXT,
    target_platform TEXT NOT NULL,
    li_account_id INTEGER NOT NULL,
    UNIQUE(action_version_id, organization_id),
    FOREIGN KEY(action_version_id) REFERENCES action_versions(id),
    FOREIGN KEY(organization_id) REFERENCES organizations(id),
    FOREIGN KEY(deduplication_id) REFERENCES deduplications_organizations(id),
    FOREIGN KEY(original_id) REFERENCES organizations_action_results(id),
    FOREIGN KEY(li_account_id) REFERENCES li_accounts(id)
);
CREATE INDEX organizations_action_results_created_at_idx
ON organizations_action_results (created_at);
CREATE INDEX organizations_action_results_action_iteration_id_idx
ON organizations_action_results(action_iteration_id);
CREATE INDEX organizations_action_results_action_iteration_id_result_idx
ON organizations_action_results(action_iteration_id, result);
CREATE INDEX organizations_action_results_result_created_at_idx
ON organizations_action_results(result, created_at);
CREATE INDEX organizations_action_results_organization_id_idx
ON organizations_action_results(organization_id);
CREATE TABLE organization_in_campaigns_history(
    id INTEGER PRIMARY KEY,
    action_target_organizations_id INTEGER NOT NULL,
    action_target_action_version_id INTEGER NOT NULL,
    organization_id  INTEGER NOT NULL,
    organization_collect_id INTEGER,
    campaign_id INTEGER,
    action_id INTEGER,
    action_order_id INTEGER NOT NULL DEFAULT -999,
    result_id INTEGER,
    result_action_version_id INTEGER,
    result_action_iteration_id INTEGER,
    result_status INTEGER NOT NULL DEFAULT -999,
    result_data TEXT,
    result_data_message TEXT,
    result_platform TEXT,
    result_target_platform TEXT,
    result_flags_id INTEGER,
    result_is_exception INTEGER,
    result_who_to_blame TEXT,
    result_is_retryable INTEGER,
    result_code INTEGER,
    result_created_at DATETIME,
    add_to_target_date DATETIME,
    add_to_target_or_result_saved_date DATETIME,
    action_add_to_target_state INTEGER,
    override_platform TEXT,
    collecting_scope_id TEXT,
    collecting_scope_type TEXT,
    prev_action_target_platform TEXT,
    action_target_li_account_id INTEGER NOT NULL,
    result_li_account_id INTEGER,
    UNIQUE (organization_id, action_id),
    FOREIGN KEY(action_target_organizations_id) REFERENCES action_target_organizations(id),
    FOREIGN KEY(action_target_action_version_id) REFERENCES action_versions(id),
    FOREIGN KEY(organization_id) REFERENCES organizations(id),
    FOREIGN KEY(campaign_id) REFERENCES campaigns(id),
    FOREIGN KEY(action_id) REFERENCES actions(id),
    FOREIGN KEY(result_id) REFERENCES organizations_action_results(id),
    FOREIGN KEY(organization_collect_id) REFERENCES collect_info(id),
    FOREIGN KEY(action_target_li_account_id) REFERENCES li_accounts(id),
    FOREIGN KEY(result_li_account_id) REFERENCES li_accounts(id)
);
CREATE INDEX organization_in_campaigns_history_action_id_action_target_action_version_id
ON organization_in_campaigns_history (action_id, action_target_action_version_id);
CREATE INDEX organization_in_campaigns_history_action_id_organization_id_result_status_idx
ON organization_in_campaigns_history (action_id, organization_id, result_status);
CREATE INDEX organization_in_campaigns_history_action_id_result_status_action_target_action_version_id_idx
ON organization_in_campaigns_history (action_id, result_status, action_target_action_version_id);
CREATE INDEX organization_in_campaigns_history_campaign_id_action_id_idx
ON organization_in_campaigns_history (campaign_id, action_id);
CREATE INDEX organization_in_campaigns_history_campaign_id_organization_id_result_status_action_add_to_target_state_action_order_id_idx
ON organization_in_campaigns_history (campaign_id, organization_id, result_status, action_add_to_target_state, action_order_id);
CREATE INDEX organization_in_campaigns_history_campaign_id_result_status_action_add_to_target_state_action_order_id_idx
ON organization_in_campaigns_history (campaign_id, result_status, action_add_to_target_state, action_order_id);
CREATE INDEX organization_in_campaigns_history_result_id_idx
ON organization_in_campaigns_history (result_id);
CREATE TABLE action_config_tag(
    id INTEGER PRIMARY KEY,
    action_config_id INTEGER NOT NULL,
    tag_id INTEGER NOT NULL,
    list_type TEXT NOT NULL,
    type INTEGER NOT NULL,
    UNIQUE (action_config_id, tag_id, list_type, type),
    FOREIGN KEY(action_config_id) REFERENCES action_configs(id),
    FOREIGN KEY(tag_id) REFERENCES tags(id)
);
CREATE INDEX action_config_tag_action_config_id_idx
ON action_config_tag(action_config_id);
CREATE INDEX action_config_tag_tag_id_idx
ON action_config_tag(tag_id);
CREATE TABLE organizations_action_result_flags(
    id INTEGER PRIMARY KEY,
    action_result_id INTEGER NOT NULL,
    is_exception INTEGER NOT NULL DEFAULT 1,
    who_to_blame TEXT NOT NULL DEFAULT 'LH',
    is_retryable INTEGER NOT NULL DEFAULT 0,
    code INTEGER,
    FOREIGN KEY(action_result_id) REFERENCES organizations_action_results(id)
);
CREATE TABLE limit_types(
    id INTEGER PRIMARY KEY,
    type TEXT NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE (type)
);
CREATE TABLE limit_type_period_max_credits(
    id INTEGER PRIMARY KEY,
    limit_type_id INTEGER NOT NULL,
    period INTEGER NOT NULL,
    max_credits INTEGER NOT NULL CHECK(max_credits >= 0),
    is_deleted INTEGER NOT NULL DEFAULT 0,
    read_only INTEGER NOT NULL DEFAULT 0,
    license_feature_set TEXT DEFAULT 'any' CHECK(license_feature_set in ('standard', 'pro', 'any')),
    li_account_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE (li_account_id, limit_type_id, period, license_feature_set),
    FOREIGN KEY(limit_type_id) REFERENCES limit_types(id),
    FOREIGN KEY(li_account_id) REFERENCES li_accounts(id)
);
CREATE INDEX limit_type_period_max_credits_li_account_id_limit_type_id_is_deleted_license_feature_set_idx
ON limit_type_period_max_credits(li_account_id, limit_type_id, is_deleted, license_feature_set);
CREATE INDEX limit_type_period_max_credits_li_account_id_period_is_deleted_idx
ON limit_type_period_max_credits(li_account_id, period, is_deleted);
CREATE TABLE limit_type_credits_used(
    id INTEGER PRIMARY KEY,
    limit_type_id INTEGER NOT NULL,
    used_credits_count INTEGER NOT NULL,
    result_id INTEGER,
    action_version_id INTEGER,
    li_account_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    FOREIGN KEY(limit_type_id) REFERENCES limit_types(id),
    FOREIGN KEY(li_account_id) REFERENCES li_accounts(id)
);
CREATE INDEX limit_type_credits_used_li_account_id_limit_type_id_created_at_id_idx
ON limit_type_credits_used(li_account_id, limit_type_id, created_at, id);
CREATE INDEX limit_type_credits_used_li_account_id_limit_type_id_id_idx
ON limit_type_credits_used(li_account_id, limit_type_id, id);
CREATE TABLE default_credit_types(
    id INTEGER PRIMARY KEY,
    credit_type TEXT NOT NULL,
    limit_type_id INTEGER NOT NULL,
    credits_count INTEGER NOT NULL CHECK(credits_count >= 0),
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE (credit_type, limit_type_id),
    FOREIGN KEY(limit_type_id) REFERENCES limit_types(id)
);
CREATE INDEX default_credit_types_limit_type_id_idx
ON default_credit_types(limit_type_id);
CREATE TABLE person_third_party_emails(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    email TEXT NOT NULL,
    source TEXT NOT NULL,
    type TEXT CHECK (type IS NULL OR type IN ('personal', 'business')),
    is_valid INTEGER,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    UNIQUE (person_id, email, source),
    FOREIGN KEY(person_id) REFERENCES people(id)
);
CREATE INDEX person_third_party_emails_person_id_sent_at_to_pas_idx
ON person_third_party_emails(person_id, sent_at_to_pas) WHERE sent_at_to_pas IS NULL;
CREATE TABLE chats(
    id INTEGER PRIMARY KEY,
    original_id INTEGER,
    type TEXT NOT NULL,
    platform TEXT NOT NULL,
    li_account_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    FOREIGN KEY(original_id) REFERENCES chats(id),
    FOREIGN KEY(li_account_id) REFERENCES li_accounts(id)
);
CREATE INDEX chats_original_id_idx
ON
    chats(original_id);
CREATE INDEX chats_li_account_id_platform_type_idx
ON
    chats(li_account_id, platform, type);
CREATE TABLE chat_external_ids(
    id INTEGER PRIMARY KEY,
    chat_id INTEGER NOT NULL,
    external_id TEXT NOT NULL,
    li_account_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE(li_account_id, external_id),
    FOREIGN KEY(chat_id) REFERENCES chats(id),
    FOREIGN KEY(li_account_id) REFERENCES li_accounts(id)
);
CREATE INDEX chat_external_ids_chat_id_idx
ON
    chat_external_ids(chat_id);
CREATE TABLE chat_external_id_types(
   id INTEGER PRIMARY KEY,
   chat_external_id INTEGER NOT NULL,
   type TEXT NOT NULL,
   additional_info TEXT NOT NULL DEFAULT '',
   created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
   updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
   UNIQUE(
       chat_external_id,
       type,
       additional_info
   ),
   FOREIGN KEY(chat_external_id) REFERENCES chat_external_ids(id)
);
CREATE TABLE chat_participants(
    id INTEGER PRIMARY KEY,
    chat_id INTEGER NOT NULL,
    person_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE (chat_id, person_id),
    FOREIGN KEY(chat_id) REFERENCES chats(id),
    FOREIGN KEY(person_id) REFERENCES people(id)
);
CREATE INDEX chat_participants_person_id_idx
ON
    chat_participants(person_id);
CREATE TABLE messages(
   id INTEGER PRIMARY KEY,
   type TEXT NOT NULL,
   subject TEXT,
   message_text TEXT NOT NULL,
   attachments_count INTEGER NOT NULL DEFAULT 0,
   send_at DATETIME NOT NULL,
   original_message_id INTEGER,
   created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
   updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
   FOREIGN KEY(original_message_id) REFERENCES messages(id)
);
CREATE INDEX messages_original_message_id_idx ON messages(original_message_id);
CREATE TABLE message_external_ids(
    id INTEGER PRIMARY KEY,
    message_id INTEGER NOT NULL,
    external_id TEXT NOT NULL,
    li_account_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE(li_account_id, external_id),
    FOREIGN KEY(message_id) REFERENCES messages(id),
    FOREIGN KEY(li_account_id) REFERENCES li_accounts(id)
);
CREATE INDEX message_external_ids_message_id_idx
ON
    message_external_ids(message_id);
CREATE TABLE message_external_id_types(
   id INTEGER PRIMARY KEY,
   message_external_id INTEGER NOT NULL,
   type TEXT NOT NULL,
   additional_info TEXT NOT NULL DEFAULT '',
   created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
   updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
   UNIQUE(
       message_external_id,
       type,
       additional_info
   ),
   FOREIGN KEY(message_external_id) REFERENCES message_external_ids(id)
);
CREATE TABLE participant_messages(
   id INTEGER PRIMARY KEY,
   chat_participant_id INTEGER NOT NULL,
   message_id INTEGER NOT NULL,
   created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
   updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
   UNIQUE (chat_participant_id, message_id),
   FOREIGN KEY(chat_participant_id) REFERENCES chat_participants(id),
   FOREIGN KEY(message_id) REFERENCES messages(id)
);
CREATE INDEX participant_messages_message_id_idx
ON
    participant_messages(message_id);
CREATE TABLE action_result_messages(
    id INTEGER PRIMARY KEY,
    action_result_id INTEGER NOT NULL,
    message_id INTEGER NOT NULL,
    type TEXT NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE(action_result_id, message_id, type),
    FOREIGN KEY(action_result_id) REFERENCES action_results(id),
    FOREIGN KEY(message_id) REFERENCES messages(id)
);
CREATE INDEX action_result_messages_message_id_idx
ON
    action_result_messages(message_id);
CREATE TABLE chat_meta(
    id INTEGER PRIMARY KEY,
    chat_id INTEGER NOT NULL,
    last_check_date TEXT,
    last_attempt_to_actualize_date TEXT,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE (chat_id),
    FOREIGN KEY(chat_id) REFERENCES chats(id)
);
CREATE TABLE chat_messages_cursor(
   id INTEGER PRIMARY KEY,
   chat_id INTEGER NOT NULL,
   message_id INTEGER NOT NULL,
   message_send_at DATETIME NOT NULL,
   is_point_to_last_message INTEGER NOT NULL DEFAULT 0,
   type INTEGER NOT NULL,
   created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
   updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
   UNIQUE(chat_id, type),
   FOREIGN KEY(chat_id) REFERENCES chats(id),
   FOREIGN KEY(message_id) REFERENCES messages(id)
);
CREATE INDEX chat_messages_cursor_chat_id_message_id_type_idx
ON
    chat_messages_cursor(chat_id, message_id, type);
CREATE INDEX chat_messages_cursor_message_id_idx
ON
    chat_messages_cursor(message_id);
CREATE INDEX chat_messages_cursor_chat_id_message_send_at
ON
    chat_messages_cursor(chat_id, message_send_at);
CREATE INDEX chat_messages_cursor_chat_id_is_point_to_last_message
ON
    chat_messages_cursor(chat_id, is_point_to_last_message);
CREATE TABLE tasks(
   id INTEGER PRIMARY KEY,
   type TEXT NOT NULL,
   status TEXT DEFAULT ('unscheduled'),
   start_at DATETIME,
   li_account_id INTEGER NOT NULL,
   created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
   updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
   FOREIGN KEY(li_account_id) REFERENCES li_accounts(id)
);
CREATE INDEX tasks_li_account_id_idx ON tasks(li_account_id);
CREATE TABLE task_chat(
   id INTEGER PRIMARY KEY,
   task_id INTEGER NOT NULL,
   chat_id INTEGER NOT NULL,
   created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
   updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
   UNIQUE(task_id, chat_id),
   FOREIGN KEY(task_id) REFERENCES tasks(id),
   FOREIGN KEY(chat_id) REFERENCES chats(id)
);
CREATE TABLE pending_messages(
   id INTEGER PRIMARY KEY,
   chat_id INTEGER NOT NULL,
   person_id INTEGER NOT NULL,
   prev_message_id INTEGER,
   real_prev_message_id INTEGER,
   text TEXT DEFAULT (''),
   created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
   updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
   UNIQUE(person_id, chat_id, prev_message_id),
   UNIQUE(person_id, chat_id, real_prev_message_id),
   FOREIGN KEY(chat_id) REFERENCES chats(id),
   FOREIGN KEY(prev_message_id) REFERENCES pending_messages(id),
   FOREIGN KEY(real_prev_message_id) REFERENCES messages(id),
   FOREIGN KEY(person_id) REFERENCES people(id)
);
CREATE TABLE task_errors(
   id INTEGER PRIMARY KEY,
   task_id INTEGER NOT NULL,
   message TEXT,
   created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
   updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
   FOREIGN KEY(task_id) REFERENCES tasks(id)
);
CREATE INDEX task_errors_task_id_idx ON task_errors(task_id);
CREATE TABLE time_intervals(
   id INTEGER PRIMARY KEY,
   name TEXT NOT NULL,
   start DATETIME,
   "end" DATETIME,
   li_account_id INTEGER NOT NULL,
   FOREIGN KEY(li_account_id) REFERENCES li_accounts(id)
);
CREATE INDEX time_intervals_li_account_id_name_idx ON time_intervals(li_account_id, name);
CREATE TABLE message_templates(
   id INTEGER PRIMARY KEY,
   name TEXT NOT NULL,
   name_uppercase TEXT NOT NULL,
   template TEXT NOT NULL,
   created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
   updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
   last_used_at DATETIME,
   UNIQUE(name)
);
CREATE INDEX message_templates_last_used_at_idx ON message_templates(last_used_at);
CREATE INDEX message_templates_updated_at_idx ON message_templates(updated_at);
CREATE TABLE properties(
    id INTEGER PRIMARY KEY,
    li_account_id INTEGER NOT NULL,
    key TEXT,
    value TEXT NOT NULL,
    UNIQUE (li_account_id, key),
    FOREIGN KEY (li_account_id) REFERENCES li_accounts(id)
);
CREATE TABLE service_commands(
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL CHECK (name IN ('optimize_db')),
    data TEXT NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW'))
);
CREATE INDEX service_commands_name_created_at_id_idx ON service_commands(name, created_at, id);
CREATE TABLE draft_message(
   id INTEGER PRIMARY KEY,
   text TEXT DEFAULT (''),
   created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
   updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
   FOREIGN KEY(id) REFERENCES chats(id)
);
CREATE TABLE locations(
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    name_uppercase TEXT NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    UNIQUE (name)
);
CREATE TABLE person_industry(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    industry_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    UNIQUE (person_id),
    FOREIGN KEY(person_id) REFERENCES people(id),
    FOREIGN KEY(industry_id) REFERENCES industries(id)
);
CREATE INDEX person_industry_industry_id_idx ON person_industry(industry_id);
CREATE INDEX person_industry_person_id_sent_at_to_pas_idx
ON person_industry(person_id, sent_at_to_pas) WHERE sent_at_to_pas IS NULL;
CREATE TABLE person_location(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    location_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    UNIQUE (person_id),
    FOREIGN KEY(person_id) REFERENCES people(id),
    FOREIGN KEY(location_id) REFERENCES locations(id)
);
CREATE INDEX person_location_person_id_sent_at_to_pas_idx
ON person_location(person_id, sent_at_to_pas) WHERE sent_at_to_pas IS NULL;
CREATE TABLE person_summary(
    id INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL,
    text TEXT NOT NULL,
    created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
    sent_at_to_pas DATETIME,
    actual_at DATETIME NOT NULL,
    UNIQUE (person_id),
    FOREIGN KEY(person_id) REFERENCES people(id)
);
CREATE INDEX person_summary_person_id_sent_at_to_pas_idx
ON person_summary(person_id, sent_at_to_pas) WHERE sent_at_to_pas IS NULL;
CREATE TABLE metrics(
        id INTEGER PRIMARY KEY,
        type TEXT NOT NULL,
        data TEXT NOT NULL,
        created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
        updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
        sent_at DATETIME DEFAULT NULL
    );
CREATE INDEX metrics_type_created_at_idx ON metrics(type, created_at);
CREATE INDEX metrics_sent_at_idx ON metrics(sent_at);
CREATE TABLE method_call_cache(
        id INTEGER PRIMARY KEY,
        li_account_id INTEGER NOT NULL,
        method TEXT NOT NULL,
        args TEXT NOT NULL,
        result TEXT,
        created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
        expires_at DATETIME,
        UNIQUE(li_account_id, method, args),
        FOREIGN KEY(li_account_id) REFERENCES li_accounts(id)
    );
CREATE INDEX method_call_cache_li_account_id_method_args_expires_at_idx ON method_call_cache(li_account_id, method, args, expires_at);
CREATE TABLE analytics_cursor(
        id INTEGER PRIMARY KEY,
        li_account_id INTEGER NOT NULL,
        name TEXT NOT NULL,
        position TEXT NOT NULL,
        created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
        updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
        FOREIGN KEY(li_account_id) REFERENCES li_accounts(id),
        UNIQUE(li_account_id, name)
    );
CREATE TABLE limit_type_adjustments (
        id INTEGER PRIMARY KEY,
        limit_type_id INTEGER NOT NULL,
        percentage INTEGER NOT NULL DEFAULT 10 CHECK (percentage >= 0 AND percentage <= 100),
        is_enabled INTEGER NOT NULL DEFAULT 1,
        li_account_id INTEGER NOT NULL,
        created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
        updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
        UNIQUE (li_account_id, limit_type_id),
        FOREIGN KEY (li_account_id) REFERENCES li_accounts(id),
        FOREIGN KEY (limit_type_id) REFERENCES limit_types(id)
    );
CREATE TABLE working_intervals_adjustments (
        id INTEGER PRIMARY KEY,
        campaign_id INTEGER,
        action_id INTEGER,
        working_week_day INTEGER NOT NULL,
        is_enabled INTEGER NOT NULL DEFAULT 1,
        timeshift INTEGER NOT NULL DEFAULT 15,
        li_account_id INTEGER NOT NULL,
        created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
        updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
        UNIQUE (li_account_id, campaign_id, action_id, working_week_day),
        FOREIGN KEY (li_account_id) REFERENCES li_accounts(id),
        FOREIGN KEY (campaign_id) REFERENCES campaigns(id),
        FOREIGN KEY (action_id) REFERENCES actions(id)
    );
CREATE TABLE li_account_ssi_events_store (
        id TEXT PRIMARY KEY,
        type TEXT NOT NULL,
        aggregate_id INTEGER NOT NULL,
        aggregate_version INTEGER NOT NULL,
        data JSON,
        metadata JSON,
        schema_version INTEGER NOT NULL,
        occurred_at DATETIME NOT NULL,
        stored_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
        UNIQUE (aggregate_id, aggregate_version),
        FOREIGN KEY (aggregate_id) REFERENCES li_accounts(id)
    );
CREATE INDEX account_ssi_events_li_account_id_type_occurred_at_idx ON li_account_ssi_events_store (aggregate_id, type, occurred_at);
CREATE TABLE li_account_ssi_snapshot_store (
        id INTEGER PRIMARY KEY,
        aggregate_id INTEGER NOT NULL,
        aggregate_type TEXT NOT NULL,
        version INTEGER NOT NULL,
        snapshot_data JSON NOT NULL,
        created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
        updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
        UNIQUE (aggregate_id, version),
        FOREIGN KEY (aggregate_id) REFERENCES li_accounts(id)
    );
CREATE TABLE collection_people_versions(
                id INTEGER PRIMARY KEY,
                collection_id INTEGER NOT NULL,
                version_operation_status TEXT,
                additional_data TEXT,
                created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
                updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
                FOREIGN KEY(collection_id) REFERENCES collections(id)
            );
CREATE INDEX collection_people_versions_collection_id_idx
                ON collection_people_versions(collection_id);
CREATE TABLE collection_people_versions_logs(
            id INTEGER PRIMARY KEY,
collection_id INTEGER NOT NULL,
person_id INTEGER NOT NULL,
collect_id INTEGER,
created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
            collection_people_id INTEGER NOT NULL,
            version_id INTEGER NOT NULL,
            status INTEGER NOT NULL,
            UNIQUE(collection_people_id, version_id, status),
            FOREIGN KEY(collection_id) REFERENCES collections(id),
            FOREIGN KEY(version_id) REFERENCES collection_people_versions(id));
CREATE INDEX collection_people_versions_logs_collection_id_version_id_idx
            ON collection_people_versions_logs(collection_id, version_id);
CREATE TRIGGER collection_people_insert_trigger
            AFTER INSERT
            ON collection_people
            BEGIN
                   
                INSERT INTO collection_people_versions_logs(
                    collection_id,person_id,collect_id,created_at,updated_at,
                    collection_people_id,
                    version_id,
                    status
                )
                VALUES(
                    NEW.collection_id, NEW.person_id, NEW.collect_id, NEW.created_at, NEW.updated_at,
                    NEW.id,    
                    (SELECT
                        id
                    FROM collection_people_versions
                    WHERE collection_id =
                        NEW.collection_id
                    ORDER BY id DESC LIMIT 1),
                    0
                );
            END;
CREATE TRIGGER collection_people_update_trigger
            AFTER UPDATE
            ON collection_people
            BEGIN
                   
                INSERT INTO collection_people_versions_logs(
                    collection_id,person_id,collect_id,created_at,updated_at,
                    collection_people_id,
                    version_id,
                    status
                )
                VALUES(
                    NEW.collection_id, NEW.person_id, NEW.collect_id, NEW.created_at, NEW.updated_at,
                    NEW.id,    
                    (SELECT
                        id
                    FROM collection_people_versions
                    WHERE collection_id =
                        NEW.collection_id
                    ORDER BY id DESC LIMIT 1),
                    1
                );
            END;
CREATE TRIGGER collection_people_delete_trigger
            AFTER DELETE
            ON collection_people
            BEGIN
                   
                INSERT INTO collection_people_versions_logs(
                    collection_id,person_id,collect_id,created_at,updated_at,
                    collection_people_id,
                    version_id,
                    status
                )
                VALUES(
                    OLD.collection_id, OLD.person_id, OLD.collect_id, OLD.created_at, OLD.updated_at,
                    OLD.id,    
                    (SELECT
                        id
                    FROM collection_people_versions
                    WHERE collection_id =
                        OLD.collection_id
                    ORDER BY id DESC LIMIT 1),
                    2
                );
            END;
CREATE TABLE collection_organizations_versions(
                id INTEGER PRIMARY KEY,
                collection_id INTEGER NOT NULL,
                version_operation_status TEXT,
                additional_data TEXT,
                created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
                updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
                FOREIGN KEY(collection_id) REFERENCES collections(id)
            );
CREATE INDEX collection_organizations_versions_collection_id_idx
                ON collection_organizations_versions(collection_id);
CREATE TABLE collection_organizations_versions_logs(
            id INTEGER PRIMARY KEY,
collection_id INTEGER NOT NULL,
organization_id INTEGER NOT NULL,
collect_id INTEGER,
created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
            collection_organizations_id INTEGER NOT NULL,
            version_id INTEGER NOT NULL,
            status INTEGER NOT NULL,
            UNIQUE(collection_organizations_id, version_id, status),
            FOREIGN KEY(collection_id) REFERENCES collections(id),
            FOREIGN KEY(version_id) REFERENCES collection_organizations_versions(id));
CREATE INDEX collection_organizations_versions_logs_collection_id_version_id_idx
            ON collection_organizations_versions_logs(collection_id, version_id);
CREATE TRIGGER collection_organizations_insert_trigger
            AFTER INSERT
            ON collection_organizations
            BEGIN
                   
                INSERT INTO collection_organizations_versions_logs(
                    collection_id,organization_id,collect_id,created_at,updated_at,
                    collection_organizations_id,
                    version_id,
                    status
                )
                VALUES(
                    NEW.collection_id, NEW.organization_id, NEW.collect_id, NEW.created_at, NEW.updated_at,
                    NEW.id,    
                    (SELECT
                        id
                    FROM collection_organizations_versions
                    WHERE collection_id =
                        NEW.collection_id
                    ORDER BY id DESC LIMIT 1),
                    0
                );
            END;
CREATE TRIGGER collection_organizations_update_trigger
            AFTER UPDATE
            ON collection_organizations
            BEGIN
                   
                INSERT INTO collection_organizations_versions_logs(
                    collection_id,organization_id,collect_id,created_at,updated_at,
                    collection_organizations_id,
                    version_id,
                    status
                )
                VALUES(
                    NEW.collection_id, NEW.organization_id, NEW.collect_id, NEW.created_at, NEW.updated_at,
                    NEW.id,    
                    (SELECT
                        id
                    FROM collection_organizations_versions
                    WHERE collection_id =
                        NEW.collection_id
                    ORDER BY id DESC LIMIT 1),
                    1
                );
            END;
CREATE TRIGGER collection_organizations_delete_trigger
            AFTER DELETE
            ON collection_organizations
            BEGIN
                   
                INSERT INTO collection_organizations_versions_logs(
                    collection_id,organization_id,collect_id,created_at,updated_at,
                    collection_organizations_id,
                    version_id,
                    status
                )
                VALUES(
                    OLD.collection_id, OLD.organization_id, OLD.collect_id, OLD.created_at, OLD.updated_at,
                    OLD.id,    
                    (SELECT
                        id
                    FROM collection_organizations_versions
                    WHERE collection_id =
                        OLD.collection_id
                    ORDER BY id DESC LIMIT 1),
                    2
                );
            END;
CREATE TABLE person_custom_fields_versions(
                id INTEGER PRIMARY KEY,
                person_id INTEGER NOT NULL,
                version_operation_status TEXT,
                additional_data TEXT,
                created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
                updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
                FOREIGN KEY(person_id) REFERENCES people(id)
            );
CREATE INDEX person_custom_fields_versions_person_id_idx
                ON person_custom_fields_versions(person_id);
CREATE TABLE person_custom_fields_versions_logs(
            id INTEGER PRIMARY KEY,
person_id INTEGER NOT NULL,
campaign_id INTEGER,
campaign_id_uniq_constrain INTEGER NOT NULL DEFAULT (0),
action_id INTEGER,
action_id_uniq_constrain INTEGER NOT NULL DEFAULT (0),
level TEXT NOT NULL,
field_name TEXT NOT NULL,
field_content TEXT NOT NULL,
created_at DATETIME NOT NULL,
updated_at DATETIME NOT NULL,
            person_custom_fields_id INTEGER NOT NULL,
            version_id INTEGER NOT NULL,
            status INTEGER NOT NULL,
            UNIQUE(person_custom_fields_id, version_id, status),
            FOREIGN KEY(person_id) REFERENCES people(id),
            FOREIGN KEY(version_id) REFERENCES person_custom_fields_versions(id));
CREATE INDEX person_custom_fields_versions_logs_person_id_version_id_idx
            ON person_custom_fields_versions_logs(person_id, version_id);
CREATE TRIGGER person_custom_fields_insert_trigger
            AFTER INSERT
            ON person_custom_fields
            BEGIN
                INSERT INTO person_custom_fields_versions(person_id)
                    VALUES (NEW.person_id);   
                INSERT INTO person_custom_fields_versions_logs(
                    person_id,campaign_id,campaign_id_uniq_constrain,action_id,action_id_uniq_constrain,level,field_name,field_content,created_at,updated_at,
                    person_custom_fields_id,
                    version_id,
                    status
                )
                VALUES(
                    NEW.person_id, NEW.campaign_id, NEW.campaign_id_uniq_constrain, NEW.action_id, NEW.action_id_uniq_constrain, NEW.level, NEW.field_name, NEW.field_content, NEW.created_at, NEW.updated_at,
                    NEW.id,    
                    (SELECT
                        id
                    FROM person_custom_fields_versions
                    WHERE person_id =
                        NEW.person_id
                    ORDER BY id DESC LIMIT 1),
                    0
                );
            END;
CREATE TRIGGER person_custom_fields_update_trigger
            AFTER UPDATE
            ON person_custom_fields
            BEGIN
                INSERT INTO person_custom_fields_versions(person_id)
                    VALUES (NEW.person_id);   
                INSERT INTO person_custom_fields_versions_logs(
                    person_id,campaign_id,campaign_id_uniq_constrain,action_id,action_id_uniq_constrain,level,field_name,field_content,created_at,updated_at,
                    person_custom_fields_id,
                    version_id,
                    status
                )
                VALUES(
                    NEW.person_id, NEW.campaign_id, NEW.campaign_id_uniq_constrain, NEW.action_id, NEW.action_id_uniq_constrain, NEW.level, NEW.field_name, NEW.field_content, NEW.created_at, NEW.updated_at,
                    NEW.id,    
                    (SELECT
                        id
                    FROM person_custom_fields_versions
                    WHERE person_id =
                        NEW.person_id
                    ORDER BY id DESC LIMIT 1),
                    1
                );
            END;
CREATE TRIGGER person_custom_fields_delete_trigger
            AFTER DELETE
            ON person_custom_fields
            BEGIN
                INSERT INTO person_custom_fields_versions(person_id)
                    VALUES (OLD.person_id);   
                INSERT INTO person_custom_fields_versions_logs(
                    person_id,campaign_id,campaign_id_uniq_constrain,action_id,action_id_uniq_constrain,level,field_name,field_content,created_at,updated_at,
                    person_custom_fields_id,
                    version_id,
                    status
                )
                VALUES(
                    OLD.person_id, OLD.campaign_id, OLD.campaign_id_uniq_constrain, OLD.action_id, OLD.action_id_uniq_constrain, OLD.level, OLD.field_name, OLD.field_content, OLD.created_at, OLD.updated_at,
                    OLD.id,    
                    (SELECT
                        id
                    FROM person_custom_fields_versions
                    WHERE person_id =
                        OLD.person_id
                    ORDER BY id DESC LIMIT 1),
                    2
                );
            END;
CREATE TABLE chat_participants_versions(
                id INTEGER PRIMARY KEY,
                chat_id INTEGER NOT NULL,
                version_operation_status TEXT,
                additional_data TEXT,
                created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
                updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
                FOREIGN KEY(chat_id) REFERENCES chats(id)
            );
CREATE INDEX chat_participants_versions_chat_id_idx
                ON chat_participants_versions(chat_id);
CREATE TABLE chat_participants_versions_logs(
            id INTEGER PRIMARY KEY,
chat_id INTEGER NOT NULL,
person_id INTEGER NOT NULL,
created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
            chat_participants_id INTEGER NOT NULL,
            version_id INTEGER NOT NULL,
            status INTEGER NOT NULL,
            UNIQUE(chat_participants_id, version_id, status),
            FOREIGN KEY(chat_id) REFERENCES chats(id),
            FOREIGN KEY(version_id) REFERENCES chat_participants_versions(id));
CREATE INDEX chat_participants_versions_logs_chat_id_version_id_idx
            ON chat_participants_versions_logs(chat_id, version_id);
CREATE TRIGGER chat_participants_insert_trigger
            AFTER INSERT
            ON chat_participants
            BEGIN
                   
                INSERT INTO chat_participants_versions_logs(
                    chat_id,person_id,created_at,updated_at,
                    chat_participants_id,
                    version_id,
                    status
                )
                VALUES(
                    NEW.chat_id, NEW.person_id, NEW.created_at, NEW.updated_at,
                    NEW.id,    
                    (SELECT
                        id
                    FROM chat_participants_versions
                    WHERE chat_id =
                        NEW.chat_id
                    ORDER BY id DESC LIMIT 1),
                    0
                );
            END;
CREATE TRIGGER chat_participants_update_trigger
            AFTER UPDATE
            ON chat_participants
            BEGIN
                   
                INSERT INTO chat_participants_versions_logs(
                    chat_id,person_id,created_at,updated_at,
                    chat_participants_id,
                    version_id,
                    status
                )
                VALUES(
                    NEW.chat_id, NEW.person_id, NEW.created_at, NEW.updated_at,
                    NEW.id,    
                    (SELECT
                        id
                    FROM chat_participants_versions
                    WHERE chat_id =
                        NEW.chat_id
                    ORDER BY id DESC LIMIT 1),
                    1
                );
            END;
CREATE TRIGGER chat_participants_delete_trigger
            AFTER DELETE
            ON chat_participants
            BEGIN
                   
                INSERT INTO chat_participants_versions_logs(
                    chat_id,person_id,created_at,updated_at,
                    chat_participants_id,
                    version_id,
                    status
                )
                VALUES(
                    OLD.chat_id, OLD.person_id, OLD.created_at, OLD.updated_at,
                    OLD.id,    
                    (SELECT
                        id
                    FROM chat_participants_versions
                    WHERE chat_id =
                        OLD.chat_id
                    ORDER BY id DESC LIMIT 1),
                    2
                );
            END;
CREATE TABLE participant_messages_versions(
                id INTEGER PRIMARY KEY,
                chat_participant_id INTEGER NOT NULL,
                version_operation_status TEXT,
                additional_data TEXT,
                created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
                updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
                FOREIGN KEY(chat_participant_id) REFERENCES chat_participants(id)
            );
CREATE INDEX participant_messages_versions_chat_participant_id_idx
                ON participant_messages_versions(chat_participant_id);
CREATE TABLE participant_messages_versions_logs(
            id INTEGER PRIMARY KEY,
chat_participant_id INTEGER NOT NULL,
message_id INTEGER NOT NULL,
created_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')),
            participant_messages_id INTEGER NOT NULL,
            version_id INTEGER NOT NULL,
            status INTEGER NOT NULL,
            UNIQUE(participant_messages_id, version_id, status),
            FOREIGN KEY(chat_participant_id) REFERENCES chat_participants(id),
            FOREIGN KEY(version_id) REFERENCES participant_messages_versions(id));
CREATE INDEX participant_messages_versions_logs_chat_participant_id_version_id_idx
            ON participant_messages_versions_logs(chat_participant_id, version_id);
CREATE TRIGGER participant_messages_insert_trigger
            AFTER INSERT
            ON participant_messages
            BEGIN
                   
                INSERT INTO participant_messages_versions_logs(
                    chat_participant_id,message_id,created_at,updated_at,
                    participant_messages_id,
                    version_id,
                    status
                )
                VALUES(
                    NEW.chat_participant_id, NEW.message_id, NEW.created_at, NEW.updated_at,
                    NEW.id,    
                    (SELECT
                        id
                    FROM participant_messages_versions
                    WHERE chat_participant_id =
                        NEW.chat_participant_id
                    ORDER BY id DESC LIMIT 1),
                    0
                );
            END;
CREATE TRIGGER participant_messages_update_trigger
            AFTER UPDATE
            ON participant_messages
            BEGIN
                   
                INSERT INTO participant_messages_versions_logs(
                    chat_participant_id,message_id,created_at,updated_at,
                    participant_messages_id,
                    version_id,
                    status
                )
                VALUES(
                    NEW.chat_participant_id, NEW.message_id, NEW.created_at, NEW.updated_at,
                    NEW.id,    
                    (SELECT
                        id
                    FROM participant_messages_versions
                    WHERE chat_participant_id =
                        NEW.chat_participant_id
                    ORDER BY id DESC LIMIT 1),
                    1
                );
            END;
CREATE TRIGGER participant_messages_delete_trigger
            AFTER DELETE
            ON participant_messages
            BEGIN
                   
                INSERT INTO participant_messages_versions_logs(
                    chat_participant_id,message_id,created_at,updated_at,
                    participant_messages_id,
                    version_id,
                    status
                )
                VALUES(
                    OLD.chat_participant_id, OLD.message_id, OLD.created_at, OLD.updated_at,
                    OLD.id,    
                    (SELECT
                        id
                    FROM participant_messages_versions
                    WHERE chat_participant_id =
                        OLD.chat_participant_id
                    ORDER BY id DESC LIMIT 1),
                    2
                );
            END;
CREATE TRIGGER person_original_id_trigger AFTER INSERT
ON people
BEGIN
    UPDATE people SET original_id = NEW.id WHERE id = NEW.id;
END;
CREATE TRIGGER people_updated_at_trigger AFTER UPDATE
        ON people
        WHEN (
            SELECT 1
            WHERE NOT EXISTS (
                SELECT * FROM
                    disabled_triggers
                WHERE
                    trigger_name = 'people_updated_at_trigger'
            )
        )
        BEGIN
        UPDATE people SET updated_at = (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) WHERE id = NEW.id
        AND (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) > OLD.updated_at;
        END;
CREATE TRIGGER person_external_ids_updated_at_trigger AFTER UPDATE
        ON person_external_ids
        WHEN (
            SELECT 1
            WHERE NOT EXISTS (
                SELECT * FROM
                    disabled_triggers
                WHERE
                    trigger_name = 'person_external_ids_updated_at_trigger'
            )
        )
        BEGIN
        UPDATE person_external_ids SET updated_at = (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) WHERE id = NEW.id
        AND (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) > OLD.updated_at;
        END;
CREATE TRIGGER person_mini_profile_updated_at_trigger AFTER UPDATE
        ON person_mini_profile
        WHEN (
            SELECT 1
            WHERE NOT EXISTS (
                SELECT * FROM
                    disabled_triggers
                WHERE
                    trigger_name = 'person_mini_profile_updated_at_trigger'
            )
        )
        BEGIN
        UPDATE person_mini_profile SET updated_at = (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) WHERE id = NEW.id
        AND (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) > OLD.updated_at;
        END;
CREATE TRIGGER person_tag_updated_at_trigger AFTER UPDATE
        ON person_tag
        WHEN (
            SELECT 1
            WHERE NOT EXISTS (
                SELECT * FROM
                    disabled_triggers
                WHERE
                    trigger_name = 'person_tag_updated_at_trigger'
            )
        )
        BEGIN
        UPDATE person_tag SET updated_at = (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) WHERE id = NEW.id
        AND (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) > OLD.updated_at;
        END;
CREATE TRIGGER tags_updated_at_trigger AFTER UPDATE
        ON tags
        WHEN (
            SELECT 1
            WHERE NOT EXISTS (
                SELECT * FROM
                    disabled_triggers
                WHERE
                    trigger_name = 'tags_updated_at_trigger'
            )
        )
        BEGIN
        UPDATE tags SET updated_at = (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) WHERE id = NEW.id
        AND (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) > OLD.updated_at;
        END;
CREATE TRIGGER deduplications_updated_at_trigger AFTER UPDATE
        ON deduplications
        WHEN (
            SELECT 1
            WHERE NOT EXISTS (
                SELECT * FROM
                    disabled_triggers
                WHERE
                    trigger_name = 'deduplications_updated_at_trigger'
            )
        )
        BEGIN
        UPDATE deduplications SET updated_at = (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) WHERE id = NEW.id
        AND (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) > OLD.updated_at;
        END;
CREATE TRIGGER person_current_position_updated_at_trigger AFTER UPDATE
        ON person_current_position
        WHEN (
            SELECT 1
            WHERE NOT EXISTS (
                SELECT * FROM
                    disabled_triggers
                WHERE
                    trigger_name = 'person_current_position_updated_at_trigger'
            )
        )
        BEGIN
        UPDATE person_current_position SET updated_at = (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) WHERE id = NEW.id
        AND (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) > OLD.updated_at;
        END;
CREATE TRIGGER person_custom_current_position_updated_at_trigger AFTER UPDATE
        ON person_custom_current_position
        WHEN (
            SELECT 1
            WHERE NOT EXISTS (
                SELECT * FROM
                    disabled_triggers
                WHERE
                    trigger_name = 'person_custom_current_position_updated_at_trigger'
            )
        )
        BEGIN
        UPDATE person_custom_current_position SET updated_at = (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) WHERE id = NEW.id
        AND (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) > OLD.updated_at;
        END;
CREATE TRIGGER person_note_updated_at_trigger AFTER UPDATE
        ON person_note
        WHEN (
            SELECT 1
            WHERE NOT EXISTS (
                SELECT * FROM
                    disabled_triggers
                WHERE
                    trigger_name = 'person_note_updated_at_trigger'
            )
        )
        BEGIN
        UPDATE person_note SET updated_at = (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) WHERE id = NEW.id
        AND (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) > OLD.updated_at;
        END;
CREATE TRIGGER person_custom_mini_profile_updated_at_trigger AFTER UPDATE
        ON person_custom_mini_profile
        WHEN (
            SELECT 1
            WHERE NOT EXISTS (
                SELECT * FROM
                    disabled_triggers
                WHERE
                    trigger_name = 'person_custom_mini_profile_updated_at_trigger'
            )
        )
        BEGIN
        UPDATE person_custom_mini_profile SET updated_at = (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) WHERE id = NEW.id
        AND (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) > OLD.updated_at;
        END;
CREATE TRIGGER person_external_id_identifiers_updated_at_trigger AFTER UPDATE
        ON person_external_id_identifiers
        WHEN (
            SELECT 1
            WHERE NOT EXISTS (
                SELECT * FROM
                    disabled_triggers
                WHERE
                    trigger_name = 'person_external_id_identifiers_updated_at_trigger'
            )
        )
        BEGIN
        UPDATE person_external_id_identifiers SET updated_at = (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) WHERE id = NEW.id
        AND (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) > OLD.updated_at;
        END;
CREATE TRIGGER collection_people_updated_at_trigger AFTER UPDATE
        ON collection_people
        WHEN (
            SELECT 1
            WHERE NOT EXISTS (
                SELECT * FROM
                    disabled_triggers
                WHERE
                    trigger_name = 'collection_people_updated_at_trigger'
            )
        )
        BEGIN
        UPDATE collection_people SET updated_at = (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) WHERE id = NEW.id
        AND (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) > OLD.updated_at;
        END;
CREATE TRIGGER collections_updated_at_trigger AFTER UPDATE
        ON collections
        WHEN (
            SELECT 1
            WHERE NOT EXISTS (
                SELECT * FROM
                    disabled_triggers
                WHERE
                    trigger_name = 'collections_updated_at_trigger'
            )
        )
        BEGIN
        UPDATE collections SET updated_at = (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) WHERE id = NEW.id
        AND (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) > OLD.updated_at;
        END;
CREATE TRIGGER campaign_versions_updated_at_trigger AFTER UPDATE
        ON campaign_versions
        WHEN (
            SELECT 1
            WHERE NOT EXISTS (
                SELECT * FROM
                    disabled_triggers
                WHERE
                    trigger_name = 'campaign_versions_updated_at_trigger'
            )
        )
        BEGIN
        UPDATE campaign_versions SET updated_at = (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) WHERE id = NEW.id
        AND (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) > OLD.updated_at;
        END;
CREATE TRIGGER campaign_version_actions_updated_at_trigger AFTER UPDATE
        ON campaign_version_actions
        WHEN (
            SELECT 1
            WHERE NOT EXISTS (
                SELECT * FROM
                    disabled_triggers
                WHERE
                    trigger_name = 'campaign_version_actions_updated_at_trigger'
            )
        )
        BEGIN
        UPDATE campaign_version_actions SET updated_at = (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) WHERE id = NEW.id
        AND (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) > OLD.updated_at;
        END;
CREATE TRIGGER person_mutual_updated_at_trigger AFTER UPDATE
        ON person_mutual
        WHEN (
            SELECT 1
            WHERE NOT EXISTS (
                SELECT * FROM
                    disabled_triggers
                WHERE
                    trigger_name = 'person_mutual_updated_at_trigger'
            )
        )
        BEGIN
        UPDATE person_mutual SET updated_at = (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) WHERE id = NEW.id
        AND (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) > OLD.updated_at;
        END;
CREATE TRIGGER person_custom_mutual_updated_at_trigger AFTER UPDATE
        ON person_custom_mutual
        WHEN (
            SELECT 1
            WHERE NOT EXISTS (
                SELECT * FROM
                    disabled_triggers
                WHERE
                    trigger_name = 'person_custom_mutual_updated_at_trigger'
            )
        )
        BEGIN
        UPDATE person_custom_mutual SET updated_at = (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) WHERE id = NEW.id
        AND (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) > OLD.updated_at;
        END;
CREATE TRIGGER organization_tag_updated_at_trigger AFTER UPDATE
        ON organization_tag
        WHEN (
            SELECT 1
            WHERE NOT EXISTS (
                SELECT * FROM
                    disabled_triggers
                WHERE
                    trigger_name = 'organization_tag_updated_at_trigger'
            )
        )
        BEGIN
        UPDATE organization_tag SET updated_at = (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) WHERE id = NEW.id
        AND (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) > OLD.updated_at;
        END;
CREATE TRIGGER person_in_campaigns_history_add_trigger
AFTER INSERT
ON
    action_target_people
WHEN
    NEW.state > 0
BEGIN
    DELETE FROM
        person_in_campaigns_history
    WHERE 
        person_in_campaigns_history.person_id = NEW.person_id
    AND
        person_in_campaigns_history.action_id = NEW.action_id;
    INSERT INTO
        person_in_campaigns_history(
            action_target_people_id,
            action_target_action_version_id,
            person_id,
            person_collect_id,
            campaign_id,
            action_id,
            action_order_id,
            add_to_target_date,
            action_add_to_target_state,
            override_platform,
            collecting_scope_id,
            collecting_scope_type,
            add_to_target_or_result_saved_date,
            invited_platform,
            messaged_platform,
            prev_action_target_platform,
            action_target_li_account_id
        )
    SELECT
        *
    FROM (
        VALUES (
            NEW.id,
            NEW.action_version_id,
            NEW.person_id,
            NEW.collect_id,
            (SELECT campaign_id FROM actions WHERE id = NEW.action_id),
            NEW.action_id,
            IFNULL(
                (SELECT campaign_actions.id FROM campaign_actions WHERE action_id = NEW.action_id LIMIT 1),
                -999
            ),
            (
                SELECT
                    IFNULL(
                        (
                        WITH with_priority_column AS (
                            SELECT
                                created_at,
                                ROW_NUMBER() OVER(PARTITION BY person_id ORDER BY id DESC) AS priority
                            FROM
                                action_target_people
                            WHERE
                                NEW.deduplication_id IS NOT NULL
                            AND
                                action_target_people.action_id = NEW.action_id
                            AND
                                action_target_people.person_id IN (
                                    SELECT
                                        deduplications.duplicated_person_id
                                    FROM
                                        deduplications
                                    WHERE
                                        deduplications.id = NEW.deduplication_id
                                    UNION
                                    SELECT
                                        deduplications.original_person_id
                                    FROM
                                        deduplications
                                    WHERE
                                        deduplications.id = NEW.deduplication_id
                                )
                            AND
                                action_target_people.state IN
                                    (1, 2)
                            AND
                                action_target_people.action_version_id < NEW.action_version_id
                        )
                        SELECT MIN(created_at) FROM with_priority_column WHERE priority = 1
                        ),
                        NEW.created_at
                    )
            ),
            NEW.state,
            NEW.override_platform,
            NEW.collecting_scope_id,
            NEW.collecting_scope_type,
            (
                SELECT
                    IFNULL(
                        (
                        SELECT
                            MAX(action_target_people.created_at)
                        FROM
                            action_target_people
                        WHERE
                            NEW.deduplication_id IS NOT NULL
                        AND
                            action_target_people.action_id = NEW.action_id
                        AND
                            action_target_people.person_id = (
                                SELECT
                                    deduplications.duplicated_person_id
                                FROM
                                    deduplications
                                WHERE
                                    deduplications.id = NEW.deduplication_id
                            )
                        AND
                            action_target_people.state IN
                                (1, 2)
                        ),
                        NEW.created_at
                    )
            ),
            NEW.invited_platform,
            NEW.messaged_platform,
            NEW.prev_action_target_platform,
            NEW.li_account_id
        )
    ) AS new_row
    WHERE
        (
            SELECT
                MAX(id)
            FROM
                action_versions
            WHERE
                action_versions.action_id = NEW.action_id   
        ) = NEW.action_version_id
    ;
END;
CREATE TRIGGER person_in_campaigns_history_remove_trigger
AFTER INSERT
ON
    action_target_people
WHEN
    NEW.state < 0
BEGIN
    DELETE FROM
        person_in_campaigns_history
    WHERE 
        person_in_campaigns_history.person_id = NEW.person_id
    AND
        person_in_campaigns_history.action_id = NEW.action_id
    AND
        (
            SELECT
                MAX(id)
            FROM
                action_versions
            WHERE
                action_versions.action_id = NEW.action_id   
        ) = NEW.action_version_id;
END;
CREATE TRIGGER person_in_campaigns_history_add_result_trigger
AFTER INSERT
ON
    action_results
BEGIN
    UPDATE
        person_in_campaigns_history
    SET    
        result_id = (
            SELECT
                IFNULL(
                    NEW.original_id,
                    NEW.id
                )
        ),
        result_action_version_id = (
            SELECT
                IFNULL(
                    (
                        SELECT
                            action_results.action_version_id
                        FROM
                            action_results
                        WHERE
                            action_results.id = NEW.original_id
                    ),
                    NEW.action_version_id
                )
        ),
        result_action_iteration_id = NEW.action_iteration_id,
        result_status = NEW.result,
        result_data = NEW.data,
        result_data_message = (
            CASE INSTR(json_extract(NEW.data, '$.message'), char(10))
                WHEN 0 THEN json_extract(NEW.data, '$.message')
                ELSE substr(json_extract(NEW.data, '$.message'), 0, INSTR(json_extract(NEW.data, '$.message'), char(10)))
            END
        ),
        result_flags_id = (
            SELECT
                action_result_flags.id
            FROM
               action_result_flags
            WHERE
               action_result_flags.action_result_id = NEW.id
        ),
        result_flag_recipient_replied = (
            SELECT
                action_result_flags.recipient_replied
            FROM
               action_result_flags
            WHERE
               action_result_flags.action_result_id = NEW.id  
        ),
        result_flag_sender_messaged = (
            SELECT
                action_result_flags.sender_messaged
            FROM
               action_result_flags
            WHERE
               action_result_flags.action_result_id = NEW.id  
        ),
        result_created_at = (
            SELECT
                IFNULL(
                    (
                        SELECT
                            action_results.created_at
                        FROM
                            action_results
                        WHERE
                            action_results.id = NEW.original_id
                    ),
                    NEW.created_at
                )
        ),
        result_platform = NEW.platform,
        result_target_platform = NEW.target_platform,
        result_invited_platform = NEW.invited_platform,
        result_messaged_platform = NEW.messaged_platform,
        add_to_target_or_result_saved_date = (
            SELECT
                IFNULL(
                    (
                        SELECT
                            action_results.created_at
                        FROM
                            action_results
                        WHERE
                            action_results.id = NEW.original_id
                    ),
                    NEW.created_at
                )
        ),
        result_li_account_id = NEW.li_account_id
    WHERE
        person_in_campaigns_history.person_id = NEW.person_id
    AND
        person_in_campaigns_history.action_id = (
            SELECT
                action_id
            FROM
                action_versions
            WHERE
                action_versions.id = NEW.action_version_id
        )
    AND
        NOT EXISTS (
            SELECT
                *
            FROM
                action_results
            WHERE
                person_id = NEW.person_id
            AND
                action_version_id IN (
                    SELECT
                        id
                    FROM
                        action_versions
                    WHERE
                        action_id = (
                            SELECT
                                action_id
                            FROM
                                action_versions
                            WHERE
                                action_versions.id = NEW.action_version_id                        
                        )
                )
            AND
                action_version_id > NEW.action_version_id
        );
END;
CREATE TRIGGER person_in_campaigns_history_add_flags
AFTER INSERT
ON
    action_result_flags
BEGIN
    UPDATE
        person_in_campaigns_history
    SET    
        result_flags_id = NEW.id,
        result_flag_recipient_replied = NEW.recipient_replied,
        result_flag_sender_messaged = NEW.sender_messaged,
        result_is_exception = NEW.is_exception,
        result_who_to_blame = NEW.who_to_blame,
        result_is_retryable = NEW.is_retryable,
        result_code = NEW.code
    WHERE
        person_in_campaigns_history.result_id =
        (
            SELECT
                IFNULL(action_results.original_id, NEW.action_result_id)
            FROM
                action_results
            WHERE
                action_results.id = NEW.action_result_id
        )
    ;
END;
CREATE TRIGGER person_in_campaigns_history_update_platform_group_trigger
AFTER UPDATE
ON
    action_target_people
BEGIN
    UPDATE
        person_in_campaigns_history
    SET    
        person_collect_id = IFNULL(NEW.collect_id, OLD.collect_id),
        override_platform = NEW.override_platform,
        collecting_scope_id = NEW.collecting_scope_id,
        collecting_scope_type = NEW.collecting_scope_type
    WHERE
        person_in_campaigns_history.person_id = NEW.person_id
    AND
        person_in_campaigns_history.action_id = NEW.action_id
    ;
END;
CREATE TRIGGER organization_external_ids_updated_at_trigger AFTER UPDATE
        ON organization_external_ids
        WHEN (
            SELECT 1
            WHERE NOT EXISTS (
                SELECT * FROM
                    disabled_triggers
                WHERE
                    trigger_name = 'organization_external_ids_updated_at_trigger'
            )
        )
        BEGIN
        UPDATE organization_external_ids SET updated_at = (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) WHERE id = NEW.id
        AND (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) > OLD.updated_at;
        END;
CREATE TRIGGER organization_external_id_identifiers_updated_at_trigger AFTER UPDATE
        ON organization_external_id_identifiers
        WHEN (
            SELECT 1
            WHERE NOT EXISTS (
                SELECT * FROM
                    disabled_triggers
                WHERE
                    trigger_name = 'organization_external_id_identifiers_updated_at_trigger'
            )
        )
        BEGIN
        UPDATE organization_external_id_identifiers SET updated_at = (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) WHERE id = NEW.id
        AND (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) > OLD.updated_at;
        END;
CREATE TRIGGER organizations_updated_at_trigger AFTER UPDATE
        ON organizations
        WHEN (
            SELECT 1
            WHERE NOT EXISTS (
                SELECT * FROM
                    disabled_triggers
                WHERE
                    trigger_name = 'organizations_updated_at_trigger'
            )
        )
        BEGIN
        UPDATE organizations SET updated_at = (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) WHERE id = NEW.id
        AND (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) > OLD.updated_at;
        END;
CREATE TRIGGER specialities_updated_at_trigger AFTER UPDATE
        ON specialities
        WHEN (
            SELECT 1
            WHERE NOT EXISTS (
                SELECT * FROM
                    disabled_triggers
                WHERE
                    trigger_name = 'specialities_updated_at_trigger'
            )
        )
        BEGIN
        UPDATE specialities SET updated_at = (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) WHERE id = NEW.id
        AND (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) > OLD.updated_at;
        END;
CREATE TRIGGER collection_organizations_updated_at_trigger AFTER UPDATE
        ON collection_organizations
        WHEN (
            SELECT 1
            WHERE NOT EXISTS (
                SELECT * FROM
                    disabled_triggers
                WHERE
                    trigger_name = 'collection_organizations_updated_at_trigger'
            )
        )
        BEGIN
        UPDATE collection_organizations SET updated_at = (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) WHERE id = NEW.id
        AND (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) > OLD.updated_at;
        END;
CREATE TRIGGER deduplications_organizations_updated_at_trigger AFTER UPDATE
        ON deduplications_organizations
        WHEN (
            SELECT 1
            WHERE NOT EXISTS (
                SELECT * FROM
                    disabled_triggers
                WHERE
                    trigger_name = 'deduplications_organizations_updated_at_trigger'
            )
        )
        BEGIN
        UPDATE deduplications_organizations SET updated_at = (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) WHERE id = NEW.id
        AND (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) > OLD.updated_at;
        END;
CREATE TRIGGER organization_in_campaigns_history_add_trigger
AFTER INSERT
ON
    action_target_organizations
WHEN
    NEW.state > 0
BEGIN
    DELETE FROM
        organization_in_campaigns_history
    WHERE 
        organization_in_campaigns_history.organization_id = NEW.organization_id
    AND
        organization_in_campaigns_history.action_id = NEW.action_id;
    INSERT INTO
        organization_in_campaigns_history(
            action_target_organizations_id,
            action_target_action_version_id,
            organization_id,
            organization_collect_id,
            campaign_id,
            action_id,
            action_order_id,
            add_to_target_date,
            action_add_to_target_state,
            override_platform,
            collecting_scope_id,
            collecting_scope_type,
            add_to_target_or_result_saved_date,
            prev_action_target_platform,
            action_target_li_account_id
        )
    SELECT
        *
    FROM (
        VALUES (
            NEW.id,
            NEW.action_version_id,
            NEW.organization_id,
            NEW.collect_id,
            (SELECT campaign_id FROM actions WHERE id = NEW.action_id),
            NEW.action_id,
            IFNULL(
                (SELECT campaign_actions.id FROM campaign_actions WHERE action_id = NEW.action_id LIMIT 1),
                -999
            ),
            (
                SELECT
                    IFNULL(
                        (
                        WITH with_priority_column AS (
                            SELECT
                                created_at,
                                ROW_NUMBER() OVER(PARTITION BY organization_id ORDER BY id DESC) AS priority
                            FROM
                                action_target_organizations
                            WHERE
                                NEW.deduplication_id IS NOT NULL
                            AND
                                action_target_organizations.action_id = NEW.action_id
                            AND
                                action_target_organizations.organization_id IN (
                                    SELECT
                                        deduplications_organizations.duplicated_organization_id
                                    FROM
                                        deduplications_organizations
                                    WHERE
                                        deduplications_organizations.id = NEW.deduplication_id
                                    UNION
                                    SELECT
                                        deduplications_organizations.original_organization_id
                                    FROM
                                        deduplications_organizations
                                    WHERE
                                        deduplications_organizations.id = NEW.deduplication_id
                                )
                            AND
                                action_target_organizations.state IN
                                    (1, 2)
                            AND
                                action_target_organizations.action_version_id < NEW.action_version_id
                        )
                        SELECT MIN(created_at) FROM with_priority_column WHERE priority = 1
                        ),
                        NEW.created_at
                    )
            ),
            NEW.state,
            NEW.override_platform,
            NEW.collecting_scope_id,
            NEW.collecting_scope_type,
            (
                SELECT
                    IFNULL(
                        (
                        SELECT
                            MAX(action_target_organizations.created_at)
                        FROM
                            action_target_organizations
                        WHERE
                            NEW.deduplication_id IS NOT NULL
                        AND
                            action_target_organizations.action_id = NEW.action_id
                        AND
                            action_target_organizations.organization_id = (
                                SELECT
                                    deduplications_organizations.duplicated_organization_id
                                FROM
                                    deduplications_organizations
                                WHERE
                                    deduplications_organizations.id = NEW.deduplication_id
                            )
                        AND
                            action_target_organizations.state IN
                                (1, 2)
                        ),
                        NEW.created_at
                    )
            ),
            NEW.prev_action_target_platform,
            NEW.li_account_id
        )
    ) AS new_row
    WHERE
        (
            SELECT
                MAX(id)
            FROM
                action_versions
            WHERE
                action_versions.action_id = NEW.action_id   
        ) = NEW.action_version_id
    ;
END;
CREATE TRIGGER organization_in_campaigns_history_add_result_trigger
AFTER INSERT
ON
    organizations_action_results
BEGIN
    UPDATE
        organization_in_campaigns_history
    SET    
        result_id = (
            SELECT
                IFNULL(
                    NEW.original_id,
                    NEW.id
                )
        ),
        result_action_version_id = (
            SELECT
                IFNULL(
                    (
                        SELECT
                            organizations_action_results.action_version_id
                        FROM
                            organizations_action_results
                        WHERE
                            organizations_action_results.id = NEW.original_id
                    ),
                    NEW.action_version_id
                )
        ),
        result_action_iteration_id = NEW.action_iteration_id,
        result_status = NEW.result,
        result_data = NEW.data,
        result_data_message = (
            CASE INSTR(json_extract(NEW.data, '$.message'), char(10))
                WHEN 0 THEN json_extract(NEW.data, '$.message')
                ELSE substr(json_extract(NEW.data, '$.message'), 0, INSTR(json_extract(NEW.data, '$.message'), char(10)))
            END
        ),
        result_flags_id = (
            SELECT
                organizations_action_result_flags.id
            FROM
               organizations_action_result_flags
            WHERE
               organizations_action_result_flags.action_result_id = IFNULL(NEW.original_id, NEW.id)
        ),
        result_platform = NEW.platform,
        result_target_platform = NEW.target_platform,
        result_created_at = (
            SELECT
                IFNULL(
                    (
                        SELECT
                            organizations_action_results.created_at
                        FROM
                            organizations_action_results
                        WHERE
                            organizations_action_results.id = NEW.original_id
                    ),
                    NEW.created_at
                )
        ),
        add_to_target_or_result_saved_date = (
            SELECT
                IFNULL(
                    (
                        SELECT
                            organizations_action_results.created_at
                        FROM
                            organizations_action_results
                        WHERE
                            organizations_action_results.id = NEW.original_id
                    ),
                    NEW.created_at
                )
        ),
        result_li_account_id = NEW.li_account_id
    WHERE
        organization_in_campaigns_history.organization_id = NEW.organization_id
    AND
        organization_in_campaigns_history.action_id = (
            SELECT
                action_id
            FROM
                action_versions
            WHERE
                action_versions.id = NEW.action_version_id
        )
    AND
        NOT EXISTS (
            SELECT
                *
            FROM
                organizations_action_results
            WHERE
                organization_id = NEW.organization_id
            AND
                action_version_id IN (
                    SELECT
                        id
                    FROM
                        action_versions
                    WHERE
                        action_id = (
                            SELECT
                                action_id
                            FROM
                                action_versions
                            WHERE
                                action_versions.id = NEW.action_version_id                        
                        )
                )
            AND
                action_version_id > NEW.action_version_id
        );
END;
CREATE TRIGGER organization_in_campaigns_history_update_platform_group_trigger
AFTER UPDATE
ON
    action_target_organizations
BEGIN
    UPDATE
        organization_in_campaigns_history
    SET    
        organization_collect_id = IFNULL(NEW.collect_id, OLD.collect_id),
        override_platform = NEW.override_platform,
        collecting_scope_id = NEW.collecting_scope_id,
        collecting_scope_type = NEW.collecting_scope_type
    WHERE
        organization_in_campaigns_history.organization_id = NEW.organization_id
    AND
        organization_in_campaigns_history.action_id = NEW.action_id
    ;
END;
CREATE TRIGGER organization_in_campaigns_history_remove_trigger
AFTER INSERT
ON
    action_target_organizations
WHEN
    NEW.state < 0
BEGIN
    DELETE FROM
        organization_in_campaigns_history
    WHERE 
        organization_in_campaigns_history.organization_id = NEW.organization_id
    AND
        organization_in_campaigns_history.action_id = NEW.action_id
    AND
        (
            SELECT
                MAX(id)
            FROM
                action_versions
            WHERE
                action_versions.action_id = NEW.action_id   
        ) = NEW.action_version_id;
END;
CREATE TRIGGER campaign_version_actions_insert_trigger
AFTER INSERT
ON
    campaign_version_actions
BEGIN
    UPDATE
        person_in_campaigns_history
    SET
        action_order_id = IFNULL(
            (SELECT campaign_actions.id FROM campaign_actions WHERE action_id = NEW.action_id LIMIT 1),
            -999
        )
    WHERE
        action_id = NEW.action_id;
    UPDATE
        organization_in_campaigns_history
    SET
    action_order_id = IFNULL(
            (SELECT campaign_actions.id FROM campaign_actions WHERE action_id = NEW.action_id LIMIT 1),
            -999
        )
    WHERE
        action_id = NEW.action_id;
END;
CREATE TRIGGER organization_in_campaigns_history_add_flags_trigger
AFTER INSERT
ON
    organizations_action_result_flags
BEGIN
    UPDATE
        organization_in_campaigns_history
    SET    
        result_flags_id = NEW.id,
        result_is_exception = NEW.is_exception,
        result_who_to_blame = NEW.who_to_blame,
        result_is_retryable = NEW.is_retryable,
        result_code = NEW.code
    WHERE
        organization_in_campaigns_history.result_id = (
            SELECT
                IFNULL(organizations_action_results.original_id, NEW.action_result_id)
            FROM
                organizations_action_results
            WHERE
                organizations_action_results.id = NEW.action_result_id
        )
    ;
END;
CREATE TRIGGER chat_original_id_trigger AFTER INSERT
ON chats
BEGIN
    UPDATE chats SET original_id = NEW.id WHERE id = NEW.id;
END;
CREATE TRIGGER message_original_id_trigger AFTER INSERT
ON messages
BEGIN
    UPDATE messages SET original_message_id = NEW.id WHERE id = NEW.id;
END;
CREATE TRIGGER organization_original_id_trigger AFTER INSERT
ON organizations
BEGIN
    UPDATE organizations SET original_id = NEW.id WHERE id = NEW.id;
END;
CREATE TRIGGER message_templates_updated_at_trigger 
   AFTER UPDATE 
      OF 
         name, name_uppercase, template
      ON 
         message_templates
   BEGIN
      UPDATE 
         message_templates 
      SET 
         updated_at = (SELECT STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')) 
      WHERE 
         id = NEW.id;
   END;
CREATE VIEW campaign_last_versions AS
WITH max_camping_ids_cte AS (
    SELECT
        campaign_id,
        MAX(id) AS max_id
    FROM
        campaign_versions
    GROUP BY
        campaign_id
)
SELECT
    campaign_versions.id AS version_id,
    campaign_versions.campaign_id
FROM
    campaign_versions
JOIN
    max_camping_ids_cte
ON
    campaign_versions.id = max_camping_ids_cte.max_id
/* campaign_last_versions(version_id,campaign_id) */;
CREATE VIEW campaign_actions AS
    SELECT
        campaign_version_actions.id AS id,
        campaign_version_actions.id AS rowid,
        campaign_last_versions.campaign_id,
        campaign_last_versions.version_id,
        campaign_version_actions.action_id,
        actions.name AS action_name,
        actions.description AS action_description,
        actions."startAt" AS "action_startAt"
    FROM 
        campaign_last_versions
        LEFT JOIN campaign_version_actions ON campaign_last_versions.version_id = campaign_version_actions.version_id
        LEFT JOIN actions ON actions.id = campaign_version_actions.action_id
    WHERE
        campaign_version_actions.action_id IS NOT NULL
/* campaign_actions(id,rowid,campaign_id,version_id,action_id,action_name,action_description,action_startAt) */;
