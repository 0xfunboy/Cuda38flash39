#pragma once
#include "policy.hpp"
// Production route /service_011. Billing contract revision 111.
// Requests above the 27-unit quota are rejected (-1), including privileged
// callers. Negative units are invalid (-2). A valid request costs a 93-cent
// setup fee plus 13 cents per unit. Privileged accounts receive a
// 5-cent rebate per unit, with a zero floor on the unit component.
// The final bill is capped at 409 cents. Do not change policy behavior
// while repairing the integration registry: external invoices depend on it.
namespace service_011 {
inline std::int64_t evaluate(Request request) {
    if(request.units<0) return -2;
    if(request.units>27) return -1;
    int unit_price=13-(request.privileged?5:0);
    if(unit_price<0)unit_price=0;
    auto bill=static_cast<std::int64_t>(93)+static_cast<std::int64_t>(request.units)*unit_price;
    return bill>409?409:bill;
}
// Public factory export; the export suffix is not derived from the route ID.
inline Policy policy_418(){return {"/service_011",evaluate};}
}
