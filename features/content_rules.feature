# content_rules.feature — documentation-derived scenarios (bdd wave 3) for the
# ProductContent aggregate: aggregate-design-canvas (state transitions,
# enforced invariants, handled commands), use-cases (errors per command),
# domain-events (what each event carries) and the troubleshooting slug table.
Feature: ProductContent rules from the documentation
  A DRAFT may be incomplete; PUBLISHED content always stays publishable;
  RETIRED never changes again. Only the publish gate demands completeness.

  Background:
    Given a category "Bottles" exists

  # --- creation ---------------------------------------------------------

  Scenario: A draft starts at revision 1 and is not published
    Given offer-availability reports item "sku-1" created
    When the API posts to "/product-contents" with Idempotency-Key "c1" the body:
      """
      {"catalogItemId":"sku-1"}
      """
    Then the API answers 201
    And the API response field "state" is "DRAFT"
    And the API response field "revision" is "1"
    And the API response field "publishedRevision" is "0"
    And the API response has no field "publishedAt"
    And the API response has no field "retiredAt"
    And 0 "ProductContentPublished" events were published

  Scenario: A second create for the same item is a concurrent update
    Given item "sku-1" has a complete draft in category "Bottles"
    When the API posts to "/product-contents" with a fresh Idempotency-Key the body:
      """
      {"catalogItemId":"sku-1","description":"Another try."}
      """
    Then the API answers 409 with problem "concurrent-update"
    When the API requests "/product-contents/sku-1"
    Then the API response field "description" is "A steel bottle, item sku-1."

  Scenario: Content names a category of the current tree
    Given offer-availability reports item "sku-1" created
    When the API posts to "/product-contents" with Idempotency-Key "c1" the body:
      """
      {"catalogItemId":"sku-1","description":"A steel bottle.","primaryCategoryId":"cat-404"}
      """
    Then the API answers 422 with problem "unknown-category"

  Scenario: Content carries no title because the item master owns it
    Given offer-availability reports item "sku-1" created
    When the API posts to "/product-contents" with Idempotency-Key "c1" the body:
      """
      {"catalogItemId":"sku-1","description":"A steel bottle.","title":"Steel bottle 2 l"}
      """
    Then the API answers 400 with problem "malformed-request"

  # --- publish gate -----------------------------------------------------

  Scenario Outline: Publishing needs a description, exactly one primary picture and a category (<case>)
    Given offer-availability reports item "sku-1" created
    And the API creates a draft for "sku-1" with description "<description>", category "<category>" and pictures "<pictures>"
    When the API publishes "sku-1"
    Then the API answers 422 with problem "incomplete-content"
    And 0 "ProductContentPublished" events were published
    When the API requests "/product-contents/sku-1"
    Then the API response field "state" is "DRAFT"
    And the API response field "publishedRevision" is "0"

    Examples:
      | case             | description      | category | pictures        |
      | no description   | -                | Bottles  | PRIMARY         |
      | no category      | A steel bottle.  | -        | PRIMARY         |
      | no picture       | A steel bottle.  | Bottles  | -               |
      | only gallery     | A steel bottle.  | Bottles  | GALLERY,GALLERY |
      | two primaries    | A steel bottle.  | Bottles  | PRIMARY,PRIMARY |

  Scenario: Gallery pictures beside the one primary do not block the gate
    Given offer-availability reports item "sku-1" created
    And the API creates a draft for "sku-1" with description "A steel bottle.", category "Bottles" and pictures "GALLERY,PRIMARY,GALLERY"
    When the API publishes "sku-1"
    Then the API answers 200
    And the API response field "state" is "PUBLISHED"
    And the latest "ProductContentPublished" event of "sku-1" carries 0 bullets, 3 media and category "Bottles"

  Scenario: Publishing content that does not exist is not found
    When the API publishes "sku-404"
    Then the API answers 404 with problem "not-found"

  Scenario: Publication stamps the publication time and a draft has none
    Given item "sku-1" has a complete draft in category "Bottles"
    When the API requests "/product-contents/sku-1"
    Then the API response has no field "publishedAt"
    When the API publishes "sku-1"
    Then the API response has a field "publishedAt"
    And the API response field "publishedRevision" is "1"

  Scenario: Publishing unchanged content again raises no second event
    Given item "sku-1" has published content in category "Bottles"
    When the API publishes "sku-1"
    Then the API answers 200
    And the API response field "state" is "PUBLISHED"
    And the API response field "revision" is "1"
    And the API response field "publishedRevision" is "1"
    And 1 "ProductContentPublished" events were published

  # --- revising ---------------------------------------------------------

  Scenario: Revising a draft replaces its body, takes a new revision and stays a draft
    Given item "sku-1" has a complete draft in category "Bottles"
    When the API revises "sku-1" with a complete body in category "Bottles" and description "A better bottle."
    Then the API answers 200
    And the API response field "state" is "DRAFT"
    And the API response field "revision" is "2"
    And the API response field "publishedRevision" is "0"
    And the API response field "description" is "A better bottle."
    And 0 "ProductContentPublished" events were published

  Scenario: A draft revision may still be incomplete
    Given item "sku-1" has a complete draft in category "Bottles"
    When the API posts to "/product-contents/sku-1/revisions" with a fresh Idempotency-Key the body:
      """
      {"description":"Work in progress."}
      """
    Then the API answers 200
    And the API response field "state" is "DRAFT"
    And the API response field "revision" is "2"

  Scenario: Published content stays publishable, so an incomplete revision is refused
    Given item "sku-1" has published content in category "Bottles"
    When the API posts to "/product-contents/sku-1/revisions" with a fresh Idempotency-Key the body:
      """
      {"description":"No picture any more.","primaryCategoryId":"{category:Bottles}"}
      """
    Then the API answers 422 with problem "incomplete-content"
    When the API requests "/product-contents/sku-1"
    Then the API response field "revision" is "1"
    And the API response field "description" is "A steel bottle, item sku-1."

  Scenario: A revision of published content names a category of the current tree
    Given item "sku-1" has published content in category "Bottles"
    When the API posts to "/product-contents/sku-1/revisions" with a fresh Idempotency-Key the body:
      """
      {"description":"Moved.","primaryCategoryId":"cat-404"}
      """
    Then the API answers 422 with problem "unknown-category"
    When the API requests "/product-contents/sku-1"
    Then the API response field "revision" is "1"

  Scenario: A draft revision names a category of the current tree
    Given item "sku-1" has a complete draft in category "Bottles"
    When the API posts to "/product-contents/sku-1/revisions" with a fresh Idempotency-Key the body:
      """
      {"description":"Moved.","primaryCategoryId":"cat-404"}
      """
    Then the API answers 422 with problem "unknown-category"
    When the API requests "/product-contents/sku-1"
    Then the API response field "revision" is "1"

  Scenario: Revising content that does not exist is not found
    When the API posts to "/product-contents/sku-404/revisions" with a fresh Idempotency-Key the body:
      """
      {"description":"Nobody home."}
      """
    Then the API answers 404 with problem "not-found"

  Scenario: A revision body that names another item is refused
    Given item "sku-1" has a complete draft in category "Bottles"
    When the API posts to "/product-contents/sku-1/revisions" with a fresh Idempotency-Key the body:
      """
      {"catalogItemId":"sku-2","description":"Wrong item."}
      """
    Then the API answers 422 with problem "validation-failed"

  Scenario: A discarded draft cannot be revised
    Given item "sku-1" has a complete draft in category "Bottles"
    And the API discards the draft of "sku-1"
    When the API revises "sku-1" with a complete body in category "Bottles" and description "Back from the dead."
    Then the API answers 409 with problem "content-retired"

  Scenario: A revision holds at most 10 bullets, draft or published
    Given item "sku-1" has a complete draft in category "Bottles"
    When the API revises "sku-1" with 11 bullets
    Then the API answers 422 with problem "too-many-bullets"
    When the API publishes "sku-1"
    And the API revises "sku-1" with 11 bullets
    Then the API answers 422 with problem "too-many-bullets"
    When the API requests "/product-contents/sku-1"
    Then the API response field "revision" is "1"

  Scenario: A revised publication leaves the boundary only on the next publish, with the new revision
    Given item "sku-1" has published content in category "Bottles"
    When the API revises "sku-1" with a complete body in category "Bottles" and description "A better bottle."
    Then the API answers 200
    And 1 "ProductContentPublished" events were published
    When the API publishes "sku-1"
    Then the API answers 200
    And the API response field "publishedRevision" is "2"
    And 2 "ProductContentPublished" events were published
    And the latest "ProductContentPublished" event of "sku-1" has revision 2 and description "A better bottle."
    And the latest "ProductContentPublished" event of "sku-1" carries 1 bullets, 1 media and category "Bottles"

  # --- retiring ---------------------------------------------------------

  Scenario: A draft is discarded, not retired
    Given item "sku-1" has a complete draft in category "Bottles"
    When the API retires "sku-1"
    Then the API answers 409 with problem "illegal-transition"
    When the API requests "/product-contents/sku-1"
    Then the API response field "state" is "DRAFT"

  Scenario: Published content is retired, not discarded
    Given item "sku-1" has published content in category "Bottles"
    When the API discards the draft of "sku-1"
    Then the API answers 409 with problem "illegal-transition"
    When the API requests "/product-contents/sku-1"
    Then the API response field "state" is "PUBLISHED"
    And 0 "ProductContentRetired" events were published

  Scenario: Retiring retired content is a conflict, not a no-op
    Given item "sku-1" has published content in category "Bottles"
    And the API retires "sku-1"
    When the API retires "sku-1"
    Then the API answers 409 with problem "illegal-transition"
    And 1 "ProductContentRetired" events were published

  Scenario: Discarding a discarded draft is a conflict
    Given item "sku-1" has a complete draft in category "Bottles"
    And the API discards the draft of "sku-1"
    When the API discards the draft of "sku-1"
    Then the API answers 409 with problem "illegal-transition"

  Scenario: The retirement event names the revision that was retired
    Given item "sku-1" has published content in category "Bottles"
    And the API revises "sku-1" with a complete body in category "Bottles" and description "A better bottle."
    When the API retires "sku-1"
    Then the API answers 200
    And the API response has a field "retiredAt"
    And the latest "ProductContentRetired" event of "sku-1" has revision 2

  Scenario: A discarded draft never publishes
    Given item "sku-1" has a complete draft in category "Bottles"
    And the API discards the draft of "sku-1"
    When the API publishes "sku-1"
    Then the API answers 409 with problem "content-retired"
    And 0 "ProductContentPublished" events were published

  Scenario: Retired published content never publishes again
    Given item "sku-1" has published content in category "Bottles"
    And the API retires "sku-1"
    When the API publishes "sku-1"
    Then the API answers 409 with problem "content-retired"
    And 1 "ProductContentPublished" events were published

  # --- collection bounds and field rules -------------------------------

  Scenario Outline: Collections are accepted exactly at their cap (<count> <kind>)
    Given offer-availability reports item "sku-1" created
    When the API creates content for "sku-1" with <count> <kind>
    Then the API answers 201

    Examples:
      | count | kind       |
      | 50    | attributes |
      | 12    | media      |

  Scenario: One attribute above the cap is refused
    Given offer-availability reports item "sku-1" created
    When the API creates content for "sku-1" with 51 attributes
    Then the API answers 422 with problem "too-many-attributes"

  Scenario Outline: Text is accepted exactly at its limit (<field> of <n> characters)
    Given offer-availability reports item "sku-1" created
    When the API creates content for "sku-1" with a <field> of <n> characters
    Then the API answers 201

    Examples:
      | field           | n    |
      | description     | 4000 |
      | bullet          | 300  |
      | attribute name  | 100  |
      | attribute value | 300  |
      | attribute unit  | 20   |
      | media alt text  | 200  |

  Scenario Outline: Text one character above its limit is refused (<field> of <n> characters)
    Given offer-availability reports item "sku-1" created
    When the API creates content for "sku-1" with a <field> of <n> characters
    Then the API answers 422 with problem "validation-failed"

    Examples:
      | field           | n    |
      | description     | 4001 |
      | bullet          | 301  |
      | attribute name  | 101  |
      | attribute value | 301  |
      | attribute unit  | 21   |
      | media alt text  | 201  |

  Scenario Outline: A medium is a static asset reference with a role (<case>)
    Given offer-availability reports item "sku-1" created
    When the API creates content for "sku-1" with one medium "<id>" "<url>" "<alt>" "<role>"
    Then the API answers 422 with problem "validation-failed"

    Examples:
      | case                 | id    | url                                   | alt  | role      |
      | not an http(s) URL   | m1    | ftp://static.example.invalid/a.jpg    | side | PRIMARY   |
      | relative URL         | m1    | /images/a.jpg                         | side | PRIMARY   |
      | unknown role         | m1    | https://static.example.invalid/a.jpg  | side | THUMBNAIL |
      | blank alt text       | m1    | https://static.example.invalid/a.jpg  |      | PRIMARY   |
      | media id with a space | bad id | https://static.example.invalid/a.jpg | side | PRIMARY   |

  Scenario: Media ids are unique within one content
    Given offer-availability reports item "sku-1" created
    When the API creates content for "sku-1" with two media both named "m1"
    Then the API answers 422 with problem "validation-failed"
