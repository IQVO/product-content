# category_rules.feature — documentation-derived scenarios (bdd wave 3) for the
# CategoryTree aggregate: its enforced invariants (no cycle, depth at most 4,
# unique sibling names), the path recomputation of a move or rename, the
# "every command publishes a new version" rule and the absence of a remove command.
Feature: CategoryTree rules from the documentation
  One live tree, at most four levels deep. Structural invariants are refused,
  never repaired. Every command publishes the whole tree under a new version.

  # --- depth ------------------------------------------------------------

  Scenario: The tree is four levels deep at most
    Given a category "Level1" exists
    And a category "Level2" exists under "Level1"
    And a category "Level3" exists under "Level2"
    And a category "Level4" exists under "Level3"
    When the API creates a category "Level5" under "Level4"
    Then the API answers 422 with problem "max-depth"
    And the live category tree has the paths "Level1, Level1/Level2, Level1/Level2/Level3, Level1/Level2/Level3/Level4"

  Scenario: A move never pushes a subtree below level four, and level four itself is allowed
    Given a category "Home" exists
    And a category "Kitchen" exists under "Home"
    And a category "Cookware" exists under "Kitchen"
    And a category "Garden" exists
    And a category "Tools" exists under "Garden"
    When the API moves category "Garden" under "Cookware"
    Then the API answers 422 with problem "max-depth"
    When the API moves category "Garden" under "Kitchen"
    Then the API answers 200
    And the API response field "path" is "Home/Kitchen/Garden"
    And the live category tree has the paths "Home, Home/Kitchen, Home/Kitchen/Cookware, Home/Kitchen/Garden, Home/Kitchen/Garden/Tools"

  Scenario: A leaf may move to level four
    Given a category "Home" exists
    And a category "Kitchen" exists under "Home"
    And a category "Cookware" exists under "Kitchen"
    And a category "Whisks" exists
    When the API moves category "Whisks" under "Cookware"
    Then the API answers 200
    And the API response field "path" is "Home/Kitchen/Cookware/Whisks"

  # --- cycles and unknown ids -------------------------------------------

  Scenario: A category never moves under itself
    Given a category "Drinkware" exists
    When the API moves category "Drinkware" under "Drinkware"
    Then the API answers 422 with problem "category-cycle"

  Scenario: A category is created only under a parent the tree holds
    When the API posts to "/categories" with a fresh Idempotency-Key the body:
      """
      {"parentId":"cat-404","name":"Ghost"}
      """
    Then the API answers 422 with problem "unknown-category"
    And the live category tree has the paths "-"

  Scenario: Moving or renaming a category the tree does not hold is not found
    When the API puts to "/categories/cat-404/parent" the body:
      """
      {"parentId":null}
      """
    Then the API answers 404 with problem "not-found"
    When the API puts to "/categories/cat-404/name" the body:
      """
      {"name":"Ghost"}
      """
    Then the API answers 404 with problem "not-found"

  # --- sibling names ----------------------------------------------------

  Scenario: Sibling names are compared without regard to case
    Given a category "Drinkware" exists
    When the API creates a category "drinkware"
    Then the API answers 422 with problem "duplicate-sibling-name"
    And the live category tree has the paths "Drinkware"

  Scenario: Categories under different parents may share a name
    Given a category "Drinkware" exists
    And a category "Kitchen" exists
    When the API creates a category "Steel" under "Drinkware"
    Then the API answers 201
    When the API creates a category "Steel" under "Kitchen"
    Then the API answers 201
    And the live category tree has the paths "Drinkware, Kitchen, Drinkware/Steel, Kitchen/Steel"

  Scenario: A move into a parent that already holds that name is refused
    Given a category "Kitchen" exists
    And a category "Steel" exists under "Kitchen"
    And a category "Drinkware" exists
    When the API creates a category "Steel" under "Drinkware"
    Then the API answers 201
    When the API moves category "Steel" under "Kitchen"
    Then the API answers 422 with problem "duplicate-sibling-name"
    And the live category tree has the paths "Kitchen, Drinkware, Kitchen/Steel, Drinkware/Steel"

  Scenario: A move to the root is refused when a root already has that name
    Given a category "Drinkware" exists
    And a category "Steel" exists
    When the API creates a category "Steel" under "Drinkware"
    Then the API answers 201
    When the API moves category "Steel" to the root
    Then the API answers 422 with problem "duplicate-sibling-name"

  Scenario: A rename to a sibling's name is refused, to its own name in another case it is not
    Given a category "Drinkware" exists
    And a category "Steel" exists under "Drinkware"
    And a category "Glass" exists under "Drinkware"
    When the API renames category "Glass" to "STEEL"
    Then the API answers 422 with problem "duplicate-sibling-name"
    When the API renames category "Steel" to "STEEL"
    Then the API answers 200
    And the API response field "name" is "STEEL"

  # --- paths ------------------------------------------------------------

  Scenario: A rename recomputes the paths of the whole subtree
    Given a category "Kitchen" exists
    And a category "Steel" exists under "Kitchen"
    And a category "Flasks" exists under "Steel"
    When the API renames category "Steel" to "Inox"
    Then the API answers 200
    And the API response field "path" is "Kitchen/Inox"
    And the live category tree has the paths "Kitchen, Kitchen/Inox, Kitchen/Inox/Flasks"

  Scenario: A move recomputes the paths of the whole subtree
    Given a category "Kitchen" exists
    And a category "Steel" exists under "Kitchen"
    And a category "Flasks" exists under "Steel"
    When the API moves category "Steel" to the root
    Then the API answers 200
    And the live category tree has the paths "Kitchen, Steel, Steel/Flasks"

  # --- names ------------------------------------------------------------

  Scenario: A category name is trimmed text without the path separator
    When the API posts to "/categories" with a fresh Idempotency-Key the body:
      """
      {"name":"Hot/Cold"}
      """
    Then the API answers 422 with problem "validation-failed"
    When the API posts to "/categories" with a fresh Idempotency-Key the body:
      """
      {"name":" Mugs"}
      """
    Then the API answers 422 with problem "validation-failed"
    When the API posts to "/categories" with a fresh Idempotency-Key the body:
      """
      {"name":"   "}
      """
    Then the API answers 422 with problem "validation-failed"
    And the live category tree has the paths "-"
    And 0 "CategoryTreePublished" events were published

  Scenario: A category name is accepted at 100 characters and refused at 101
    When the API creates a root category named with 100 characters
    Then the API answers 201
    When the API creates a root category named with 101 characters
    Then the API answers 422 with problem "validation-failed"

  Scenario: A rename obeys the same name rule as a create
    Given a category "Drinkware" exists
    When the API puts to "/categories/{category:Drinkware}/name" the body:
      """
      {"name":"Hot/Cold"}
      """
    Then the API answers 422 with problem "validation-failed"
    When the API puts to "/categories/{category:Drinkware}/name" the body:
      """
      {"name":""}
      """
    Then the API answers 422 with problem "validation-failed"
    And the live category tree has the paths "Drinkware"
    And 1 "CategoryTreePublished" events were published

  # --- versions and events ----------------------------------------------

  Scenario: The tree is empty at version 0 before the first category
    When the API requests "/categories"
    Then the API answers 200
    And the API response field "version" is "0"
    And the API response list "categories" has 0 items

  Scenario: Every tree command publishes the whole tree under a new version
    Given a category "Drinkware" exists
    Then the latest "CategoryTreePublished" event is version 1 of 1 categories
    When the API creates a category "Steel" under "Drinkware"
    Then the latest "CategoryTreePublished" event is version 2 of 2 categories
    When the API renames category "Steel" to "Inox"
    Then the latest "CategoryTreePublished" event is version 3 of 2 categories
    When the API moves category "Inox" to the root
    Then the latest "CategoryTreePublished" event is version 4 of 2 categories
    When the API posts to "/category-tree-publications" with a fresh Idempotency-Key
    Then the API answers 200
    And the API response field "version" is "5"
    And the latest "CategoryTreePublished" event is version 5 of 2 categories
    And 5 "CategoryTreePublished" events were published

  Scenario: A refused tree command publishes nothing and keeps the version
    Given a category "Drinkware" exists
    When the API creates a category "Drinkware"
    Then the API answers 422 with problem "duplicate-sibling-name"
    And 1 "CategoryTreePublished" events were published
    When the API requests "/categories"
    Then the API response field "version" is "1"

  Scenario: Republishing an unchanged tree sends it again under a new version
    Given a category "Drinkware" exists
    When the API posts to "/category-tree-publications" with a fresh Idempotency-Key
    And the API posts to "/category-tree-publications" with a fresh Idempotency-Key
    Then the API response field "version" is "3"
    And the latest "CategoryTreePublished" event is version 3 of 1 categories

  # --- no remove command ------------------------------------------------

  Scenario: There is no way to remove a category
    Given a category "Drinkware" exists
    When the API deletes "/categories/{category:Drinkware}"
    Then the API answers 404
    And the live category tree has the paths "Drinkware"
